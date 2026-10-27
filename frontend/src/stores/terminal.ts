/**
 * The terminal store: the shells of this window. The terminal's own window holds them while it is open, else the main
 * window. The holder keeps one xterm for each shell, fed by the shell's stream from Go, for as long as it holds the
 * shells, shown or not; the other windows keep none. Before the shells go to another window, each xterm's screen goes
 * to Go, which gives it to the next holder with the output that came after it.
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
import type { Link } from "@services/terminal/models";
import type { Place } from "@services/workspace/models";
import * as terminal from "@services/terminal/service";
import { findBinding, listPressedKeys } from "../app/commands";
import { shellsQuery } from "../queries";
import { useEditorStore } from "./editor";
import { useSettingsStore } from "./settings";
import { THIS_WINDOW, useWindowsStore } from "./windows";

/** A Console is one shell as this window draws it: its xterm, the addons the store calls, and its stream from Go. */
export type Console = {
  shell: Shell;
  xterm: Terminal;
  fit: FitAddon;
  serialize: SerializeAddon;
  stream: ReturnType<typeof Stream>;
  offset: number; // the offset of the output that xterm has drawn up to
};

/**
 * A Message is a message to Go on a shell's stream: the shell's id first, then what is typed, the terminal's new size
 * or how far xterm has drawn the output, which Go waits for once it is far ahead.
 */
type Message = { shell?: string; input?: string; binary?: string; columns?: number; rows?: number; offset?: number };

/** ANSI holds the eight ANSI colors, as xterm's theme names them; each has a bright one too. */
const ANSI = ["black", "red", "green", "yellow", "blue", "magenta", "cyan", "white"] as const;
// the commands whose shortcuts still reach the app from a terminal, as VS Code keeps its palette and its Go to file
// from its shells
const APP_COMMANDS = ["palette.open", "editor.goToFile"];
const decoder = new TextDecoder();

export const useTerminalStore = defineStore("terminal", () => {
  const editor = useEditorStore();
  const settings = useSettingsStore();
  const windows = useWindowsStore();
  const consoles = shallowReactive(new Map<string, Console>());
  // the shell shown, kept for the windows together, so that the next window to hold the shells shows the same one
  const active = useLocalStorage("pmt.terminal.active", "");
  // the find box over the active shell is open
  const findOpen = shallowRef(false);
  // the shells went to another window, and stay there until the terminal moves again
  const away = shallowRef(false);
  const holding = computed(() => !away.value && windows.resolveTerminal() === THIS_WINDOW);
  const { data: shells } = useQuery(() => ({ ...shellsQuery, enabled: holding.value }));

  // starts the given program's shell, or the default shell for ""
  const { mutate: start } = useMutation({
    mutation: async (program: string) => {
      const size = consoles.get(active.value)?.xterm;
      const shell = await terminal.Start(findFile()?.path ?? "", program, size?.cols ?? 80, size?.rows ?? 24);
      if (shell) active.value = shell.id;
    },
  });
  const { mutate: close } = useMutation({ mutation: (id: string) => terminal.Close(id) });
  const { mutate: sendFile } = useMutation({
    mutation: async () => {
      const place = findFile();
      const open = consoles.get(active.value);
      if (!place || !open) return;
      open.xterm.paste(await terminal.ResolveReference(open.shell.id, place.path));
      open.xterm.focus();
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
   * syncConsoles makes a console for each shell this window holds, and lets go of the consoles of the shells it no
   * longer does.
   */
  function syncConsoles(): void {
    const running = holding.value ? (shells.value ?? []).flatMap((shell) => (shell ? [shell] : [])) : [];
    for (const [id, open] of consoles) {
      if (running.some((shell) => shell.id === id)) continue;
      open.stream.close();
      open.xterm.dispose();
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
    const open: Console = markRaw({
      shell,
      xterm,
      fit: new FitAddon(),
      serialize: new SerializeAddon(),
      stream: Stream("terminal"),
      offset: 0,
    });
    applyStyle(open);
    const addons = [open.fit, open.serialize, new Unicode11Addon(), new WebLinksAddon((_, uri) => openAddress(uri))];
    addons.forEach((addon) => xterm.loadAddon(addon));
    xterm.unicode.activeVersion = "11";
    xterm.registerLinkProvider({ provideLinks: (row, callback) => void listLinks(open, row).then(callback) });
    xterm.attachCustomKeyEventHandler((event) => applyKey(open, event));
    const send = (message: Message): void => {
      if (open.stream.readyState === open.stream.OPEN) open.stream.send(JSON.stringify(message));
    };
    xterm.onData((input) => send({ input }));
    xterm.onBinary((binary) => send({ binary }));
    xterm.onResize(({ cols, rows }) => send({ columns: cols, rows }));
    open.stream.addEventListener("open", () => open.stream.send(JSON.stringify({ shell: shell.id })));
    let isScreen = true;
    open.stream.addEventListener("message", (event) => {
      const bytes = new Uint8Array((event as MessageEvent<ArrayBuffer>).data);
      if (isScreen) {
        isScreen = false;
        writeScreen(open, JSON.parse(decoder.decode(bytes)) as Screen);
        return;
      }
      xterm.write(bytes, () => {
        open.offset += bytes.length;
        send({ offset: open.offset });
      });
    });
    return open;
  }

  /**
   * writeScreen writes the screen Go sent for a shell at the size it was drawn at, then fits the terminal to its
   * element if it is shown.
   */
  function writeScreen(open: Console, screen: Screen): void {
    open.offset = screen.offset;
    open.xterm.resize(screen.columns, screen.rows);
    open.xterm.write(screen.text, () => {
      if (open.xterm.element) open.fit.fit();
    });
  }

  /** applyStyle sets a console's colors and font from the settings again, and fits it to its element if it is shown. */
  function applyStyle(open: Console): void {
    open.xterm.options.theme = resolveTheme();
    open.xterm.options.fontFamily = settings.codeFont().fontFamily;
    open.xterm.options.fontSize = Number(settings.value("ide.panelTextSize"));
    if (open.xterm.element) open.fit.fit();
  }

  /**
   * applyKey applies a key pressed in a shell's terminal and returns whether it goes on to the shell. The shortcuts of
   * the palette and Go to file reach the app, and Ctrl F opens the find. Ctrl C copies a selection, else goes to the
   * shell, and Ctrl Shift C copies. Ctrl V and Ctrl Shift V paste, through the page's own paste. Shift Enter sends ESC
   * CR, which adds a line in Claude Code, as its terminal setup makes VS Code send it. A key the terminal takes for
   * itself goes no further, so that no shortcut of the app's acts on it too.
   */
  function applyKey(open: Console, event: KeyboardEvent): boolean {
    const isShiftEnter = event.key === "Enter" && event.shiftKey && !event.ctrlKey && !event.altKey && !event.metaKey;
    // xterm asks again for the key's keypress, which would send a CR of its own
    if (isShiftEnter) {
      if (event.type === "keydown") open.xterm.input("\x1b\r");
      return false;
    }
    if (event.type !== "keydown" || !event.ctrlKey || event.altKey || event.metaKey) return true;
    const key = event.key.toLowerCase();
    if (key === "c" && (event.shiftKey || open.xterm.hasSelection())) {
      event.preventDefault();
      event.stopPropagation();
      void Clipboard.SetText(open.xterm.getSelection());
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

  /** listLinks lists the files a shell's output names at a line, on the line of the given row; Go finds them. */
  async function listLinks(open: Console, row: number): Promise<ILink[] | undefined> {
    const { text, cells } = readLine(open.xterm, row);
    if (!text.trim()) return undefined;
    return ((await terminal.FindLinks(open.shell.id, text)) ?? []).flatMap((link) => {
      const [start, end] = [cells[link.start], cells[link.end - 1]];
      if (!start || !end) return [];
      return [{ range: { start, end }, text: text.slice(link.start, link.end), activate: () => showLink(link) }];
    });
  }

  /**
   * showLink opens the file a link names at its line: in this window's editor, or in the main window's from the
   * terminal's.
   */
  function showLink(link: Link): void {
    const position = { lineNumber: link.line, column: Math.max(link.column, 1) };
    if (THIS_WINDOW === "terminal") windows.showElsewhere("main", link.place, position);
    else editor.show(link.place, position, "navigate");
  }

  /**
   * findFile finds the file the shells work on: the active file of this window, or of the main window from the
   * terminal's own. It is null when there is none.
   */
  const findFile = (): Place | null => editor.activeFile() ?? windows.findActive("main");

  /** show draws a shell's terminal in the given element, opening it there the first time, and fits it to it. */
  const show = (id: string, host: HTMLElement): void => {
    const open = consoles.get(id);
    if (!open) return;
    // the element shows one terminal at a time; xterm opens once, and after that its element moves
    host.replaceChildren();
    if (open.xterm.element) host.append(open.xterm.element);
    else open.xterm.open(host);
    open.fit.fit();
    open.xterm.refresh(0, open.xterm.rows - 1);
  };

  /**
   * handOver gives every shell's screen to Go for the next window that shows the shells, and lets go of them here. A
   * shell that ended meanwhile is passed over.
   */
  const handOver = async (): Promise<void> => {
    const screens = [...consoles.values()].map((open) => ({
      id: open.shell.id,
      screen: {
        text: open.serialize.serialize(),
        offset: open.offset,
        columns: open.xterm.cols,
        rows: open.xterm.rows,
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
    close,
    sendFile,
    findFile,
    show,
    handOver,
  };
});

/** resolveTheme resolves the terminal's colors from the page theme's tokens, which xterm cannot read as variables. */
function resolveTheme(): ITheme {
  const style = getComputedStyle(document.documentElement);
  const token = (name: string): string => style.getPropertyValue(`--${name}`).trim();
  return {
    background: token("bg-0"),
    foreground: token("fg-1"),
    cursor: token("fg-1"),
    cursorAccent: token("bg-0"),
    selectionBackground: `${token("accent")}40`,
    ...Object.fromEntries(
      ANSI.flatMap((name) => [
        [name, token(`ansi-${name}`)],
        [`bright${upperFirst(name)}`, token(`ansi-bright-${name}`)],
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
 * readLine reads the line of a terminal's buffer that the given row is part of, its wrapped rows joined, with the cell
 * each UTF-16 unit of its text sits in. The row and the cells count from one, as xterm's links do.
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
