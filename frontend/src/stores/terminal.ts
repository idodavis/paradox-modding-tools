/**
 * The terminal store: the shells of this window. The terminal's own window holds them while it is open, else the main
 * window does. The holder keeps one xterm per shell, fed by the shell's stream from Go, whether shown or not; other
 * windows keep none. Before the shells move to another window, each xterm's screen goes to Go, which hands it to the
 * next holder with the output that came after it.
 */
import { defineStore } from "pinia";
import { useMutation, useQuery } from "@pinia/colada";
import { FitAddon } from "@xterm/addon-fit";
import { SerializeAddon } from "@xterm/addon-serialize";
import { Unicode11Addon } from "@xterm/addon-unicode11";
import { WebLinksAddon } from "@xterm/addon-web-links";
import { Terminal, type IBufferCellPosition, type ILink, type ITheme } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";
import { useLocalStorage } from "@vueuse/core";
import { Browser, Clipboard, Stream } from "@wailsio/runtime";
import { upperFirst } from "es-toolkit";
import { computed, markRaw, shallowReactive, shallowRef, watch } from "vue";

import type { Screen, Shell } from "@internal/terminal/models";
import type { Place } from "@services/workspace/models";
import * as terminal from "@services/terminal/service";
import { findBinding, listPressedKeys } from "../app/commands";
import { shellsQuery } from "../queries";
import { useEditorStore } from "./editor";
import { useSettingsStore } from "./settings";
import { THIS_WINDOW, useWindowsStore } from "./windows";

/** A Console is one shell as this window draws it: its xterm, the addons the store calls, and its stream from Go. */
type Console = {
  shell: Shell;
  xterm: Terminal;
  fit: FitAddon;
  serialize: SerializeAddon;
  stream: ReturnType<typeof Stream>;
  offset: number; // the offset of the output that xterm has drawn up to
};

/**
 * A Message is what this window sends Go on a shell's stream: the shell's id first, then typed input, a new terminal
 * size, or the offset xterm has drawn up to, which Go waits for once it is far ahead.
 */
type Message = { shell?: string; input?: string; binary?: string; columns?: number; rows?: number; offset?: number };

/** ANSI holds the eight ANSI colors, as xterm's theme names them; each has a bright one too. */
const ANSI = ["black", "red", "green", "yellow", "blue", "magenta", "cyan", "white"] as const;
/** APP_COMMANDS holds the commands whose shortcuts still reach the app from a terminal, as in VS Code. */
const APP_COMMANDS = ["palette.open", "editor.goToFile"];
const decoder = new TextDecoder();

export const useTerminalStore = defineStore("terminal", () => {
  const editor = useEditorStore();
  const settings = useSettingsStore();
  const windows = useWindowsStore();
  const consoles = shallowReactive(new Map<string, Console>());
  // the id of the shell shown, shared by all windows, so the next window to hold the shells shows the same one
  const active = useLocalStorage("pmt.terminal.active", "");
  // whether the find box over the active shell is open
  const findOpen = shallowRef(false);
  // whether the shells went to another window, where they stay until the terminal moves again
  const away = shallowRef(false);
  const holding = computed(() => !away.value && windows.resolveTerminal() === THIS_WINDOW);
  const { data: shells } = useQuery(() => ({ ...shellsQuery, enabled: holding.value }));

  // starts the given program's shell, or the default shell for ""
  const { mutate: start } = useMutation({
    mutation: (program: string) => launch((path, columns, rows) => terminal.Start(path, program, columns, rows)),
  });
  // runs a command, a program and its arguments as given, in a new shell
  const { mutateAsync: run } = useMutation({
    mutation: (command: string[]) => launch((path, columns, rows) => terminal.Run(path, command, columns, rows)),
  });
  const { mutate: close } = useMutation({ mutation: (id: string) => terminal.Close(id) });
  const { mutate: sendFile } = useMutation({
    mutation: async () => {
      const place = findFile();
      const shellConsole = consoles.get(active.value);
      if (!place || !shellConsole) return;
      shellConsole.xterm.paste(await terminal.ResolveReference(shellConsole.shell.id, place.path));
      shellConsole.xterm.focus();
    },
  });

  watch(
    () => windows.resolveTerminal(),
    () => (away.value = false),
  );
  watch([holding, shells], syncConsoles, { immediate: true });
  // the theme reaches <html> before this runs
  watch(
    () =>
      `${JSON.stringify(settings.theme())} ${settings.codeFont().fontFamily} ${settings.value("ide.panelTextSize")}`,
    () => consoles.forEach(applyStyle),
    { flush: "post" },
  );

  /**
   * launch starts a shell through the given call, which takes the active file's path and the size of the shell shown,
   * and shows the new shell.
   */
  async function launch(begin: (path: string, columns: number, rows: number) => Promise<Shell | null>): Promise<void> {
    const size = consoles.get(active.value)?.xterm;
    const shell = await begin(findFile()?.path ?? "", size?.cols ?? 80, size?.rows ?? 24);
    if (shell) active.value = shell.id;
  }

  /** syncConsoles makes a console for each shell this window holds and closes the consoles of other shells. */
  function syncConsoles(): void {
    const running = holding.value ? (shells.value ?? []).flatMap((shell) => (shell ? [shell] : [])) : [];
    for (const [id, shellConsole] of consoles) {
      if (running.some((shell) => shell.id === id)) continue;
      shellConsole.stream.close();
      shellConsole.xterm.dispose();
      consoles.delete(id);
    }
    for (const shell of running) if (!consoles.has(shell.id)) consoles.set(shell.id, attach(shell));
    // the shell shown stays until the list has loaded and lacks it
    if (holding.value && shells.value && !consoles.has(active.value)) active.value = running.at(-1)?.id ?? "";
  }

  /** attach makes a shell's console: its xterm, and its stream from Go, which sends the screen to write first. */
  function attach(shell: Shell): Console {
    const xterm = new Terminal({
      allowProposedApi: true,
      windowsPty: shell.build ? { backend: "conpty", buildNumber: shell.build } : undefined,
      linkHandler: { activate: (_event, uri) => openAddress(uri) },
    });
    // never proxied: xterm's objects are not Vue's to watch
    const shellConsole: Console = markRaw({
      shell,
      xterm,
      fit: new FitAddon(),
      serialize: new SerializeAddon(),
      stream: Stream("terminal"),
      offset: 0,
    });
    applyStyle(shellConsole);
    const addons = [
      shellConsole.fit,
      shellConsole.serialize,
      new Unicode11Addon(),
      new WebLinksAddon((_, uri) => openAddress(uri)),
    ];
    addons.forEach((addon) => xterm.loadAddon(addon));
    xterm.unicode.activeVersion = "11";
    xterm.registerLinkProvider({ provideLinks: (row, callback) => void listLinks(shellConsole, row).then(callback) });
    xterm.attachCustomKeyEventHandler((event) => applyKey(shellConsole, event));
    const send = (message: Message): void => {
      if (shellConsole.stream.readyState === shellConsole.stream.OPEN) {
        shellConsole.stream.send(JSON.stringify(message));
      }
    };
    xterm.onData((input) => send({ input }));
    xterm.onBinary((binary) => send({ binary }));
    xterm.onResize(({ cols, rows }) => send({ columns: cols, rows }));
    shellConsole.stream.addEventListener("open", () => shellConsole.stream.send(JSON.stringify({ shell: shell.id })));
    let isScreen = true;
    shellConsole.stream.addEventListener("message", (event) => {
      const bytes = new Uint8Array((event as MessageEvent<ArrayBuffer>).data);
      if (isScreen) {
        isScreen = false;
        writeScreen(shellConsole, JSON.parse(decoder.decode(bytes)) as Screen);
        return;
      }
      xterm.write(bytes, () => {
        shellConsole.offset += bytes.length;
        send({ offset: shellConsole.offset });
      });
    });
    return shellConsole;
  }

  /**
   * writeScreen writes the screen Go sent for a shell at the size it was drawn at, then fits the terminal to its
   * element if it is shown.
   */
  function writeScreen(shellConsole: Console, screen: Screen): void {
    shellConsole.offset = screen.offset;
    shellConsole.xterm.resize(screen.columns, screen.rows);
    shellConsole.xterm.write(screen.text, () => fitConsole(shellConsole));
  }

  /**
   * fitConsole fits a console's terminal to its element, and so the shell to it. It does nothing while the element has
   * no width or height, as while its panel is laid out or hidden: a fit then makes the terminal two columns wide, and
   * the shell would wrap its own screen to them for good.
   */
  function fitConsole(shellConsole: Console): void {
    const box = shellConsole.xterm.element?.parentElement;
    if (box && box.clientWidth > 0 && box.clientHeight > 0) shellConsole.fit.fit();
  }

  /** applyStyle sets a console's colors and font from the settings again, and fits it to its element if it is shown. */
  function applyStyle(shellConsole: Console): void {
    shellConsole.xterm.options.theme = resolveTheme();
    shellConsole.xterm.options.fontFamily = settings.codeFont().fontFamily;
    shellConsole.xterm.options.fontSize = Number(settings.value("ide.panelTextSize"));
    fitConsole(shellConsole);
  }

  /**
   * applyKey handles a key pressed in a shell's terminal. It returns true if xterm should go on to handle the key.
   * Ctrl F opens the find box. Ctrl C copies a selection, else goes to the shell, and Ctrl Shift C always copies.
   * Ctrl V and Ctrl Shift V paste through the page's own paste. Shift Enter sends ESC CR, which adds a line in Claude
   * Code. The shortcuts of the palette and Go to file reach the app. A key the terminal handles itself is not passed
   * on, so no app shortcut acts on it too.
   */
  function applyKey(shellConsole: Console, event: KeyboardEvent): boolean {
    const isShiftEnter = event.key === "Enter" && event.shiftKey && !event.ctrlKey && !event.altKey && !event.metaKey;
    // xterm asks again for the key's keypress, which would send a CR of its own
    if (isShiftEnter) {
      if (event.type === "keydown") shellConsole.xterm.input("\x1b\r");
      return false;
    }
    if (event.type !== "keydown" || !event.ctrlKey || event.altKey || event.metaKey) return true;
    const key = event.key.toLowerCase();
    if (key === "c" && (event.shiftKey || shellConsole.xterm.hasSelection())) {
      event.preventDefault();
      event.stopPropagation();
      void Clipboard.SetText(shellConsole.xterm.getSelection());
      return false;
    }
    if (key === "f" && !event.shiftKey) {
      event.preventDefault();
      event.stopPropagation();
      findOpen.value = true;
      return false;
    }
    return key !== "v" && !isAppKey(event);
  }

  /** listLinks returns the file links that Go finds in the line of the given row. It is undefined for a blank line. */
  async function listLinks(shellConsole: Console, row: number): Promise<ILink[] | undefined> {
    const { text, cells } = readLine(shellConsole.xterm, row);
    if (!text.trim()) return undefined;
    return ((await terminal.FindLinks(shellConsole.shell.id, text)) ?? []).flatMap((link) => {
      const [start, end] = [cells[link.start], cells[link.end - 1]];
      if (!start || !end) return [];
      const position = { lineNumber: link.site.line, column: Math.max(link.site.range.startColumn, 1) };
      const activate = (): void => editor.show(link.site.place, position, "navigate");
      return [{ range: { start, end }, text: text.slice(link.start, link.end), activate }];
    });
  }

  /** findFile returns the file the shells work on: this window's active file, else the main window's. Null if none. */
  const findFile = (): Place | null => editor.activeFile() ?? windows.findActive("main");

  /** show draws a shell's terminal in the given element, opening it there the first time, and fits it to it. */
  const show = (id: string, host: HTMLElement): void => {
    const shellConsole = consoles.get(id);
    if (!shellConsole) return;
    // the element shows one terminal at a time; xterm opens once, and after that its element moves
    host.replaceChildren();
    if (shellConsole.xterm.element) host.append(shellConsole.xterm.element);
    else shellConsole.xterm.open(host);
    fitConsole(shellConsole);
    shellConsole.xterm.refresh(0, shellConsole.xterm.rows - 1);
  };

  /**
   * handOver gives every shell's screen to Go for the next window that holds the shells, and stops holding them here.
   * A failure, such as a shell that has ended, is ignored.
   */
  const handOver = async (): Promise<void> => {
    const screens = [...consoles.values()].map((shellConsole) => ({
      id: shellConsole.shell.id,
      screen: {
        text: shellConsole.serialize.serialize(),
        offset: shellConsole.offset,
        columns: shellConsole.xterm.cols,
        rows: shellConsole.xterm.rows,
      },
    }));
    away.value = true;
    await Promise.allSettled(screens.map(({ id, screen }) => terminal.SetScreen(id, screen)));
  };

  return {
    consoles: computed(() => [...consoles.values()]),
    active,
    findOpen,
    holding,
    start,
    run,
    close,
    sendFile,
    findFile,
    show,
    fitConsole,
    handOver,
  };
});

/** resolveTheme resolves the terminal's colors from the page theme's tokens, which xterm cannot read as variables. */
function resolveTheme(): ITheme {
  const style = getComputedStyle(document.documentElement);
  const token = (suffix: string): string => style.getPropertyValue(`--${suffix}`).trim();
  return {
    background: token("bg-0"),
    foreground: token("fg-1"),
    cursor: token("fg-1"),
    cursorAccent: token("bg-0"),
    selectionBackground: `${token("accent")}40`,
    ...Object.fromEntries(
      ANSI.flatMap((color) => [
        [color, token(`ansi-${color}`)],
        [`bright${upperFirst(color)}`, token(`ansi-bright-${color}`)],
      ]),
    ),
  };
}

/** isAppKey reports whether a key is the shortcut of a command that reaches the app from a terminal. */
function isAppKey(event: KeyboardEvent): boolean {
  const command = findBinding(listPressedKeys(event));
  return !!command && APP_COMMANDS.includes(command.id);
}

/**
 * readLine returns the text of the line that holds the given row, with its wrapped rows joined, and the cell of each
 * UTF-16 unit of the text. The row and the cells count from one, as xterm's links do.
 */
function readLine(xterm: Terminal, row: number): { text: string; cells: IBufferCellPosition[] } {
  const buffer = xterm.buffer.active;
  let first = row - 1;
  while (first > 0 && buffer.getLine(first)?.isWrapped) first--;
  let text = "";
  const cells: IBufferCellPosition[] = [];
  for (let y = first; y === first || buffer.getLine(y)?.isWrapped; y++) {
    const line = buffer.getLine(y);
    if (!line) break;
    for (let x = 0; x < line.length; x++) {
      const cell = line.getCell(x);
      // the second half of a wide character has no text of its own
      if (!cell || cell.getWidth() === 0) continue;
      const chars = cell.getChars() || " ";
      text += chars;
      for (let unit = 0; unit < chars.length; unit++) cells.push({ x: x + 1, y: y + 1 });
    }
  }
  return { text, cells };
}

/** openAddress opens a web address in the system's browser. An address that is not http or https is left alone. */
function openAddress(uri: string): void {
  if (/^https?:\/\//i.test(uri)) void Browser.OpenURL(uri);
}
