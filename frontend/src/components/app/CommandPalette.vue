<script lang="ts">
import { shallowRef } from "vue";

/** Whether the palette is open, and what its search starts with: ">" commands, "" files, "@" symbols, "#" events and
 * localization keys. */
export const paletteOpen = shallowRef(false);
const start = shallowRef("> ");

/** Open the palette: the top bar's search, Ctrl K and Ctrl Shift P on commands, Ctrl P on files. */
export function openPalette(prefix: "> " | "" = "> "): void {
  start.value = prefix;
  paletteOpen.value = true;
}
</script>

<script setup lang="ts">
/**
 * The command palette (the CommandPalette and PaletteSettings boards) on every page. After ">", the pages, the
 * workspaces, the IDE's commands and every setting: a setting that is on or off flips where you are, one with a value
 * opens the page or the card that sets it. Without it, the workspace's files; after "@", the open file's names; after
 * "#", events and on_actions, the game's included, and the localization keys starting with what is typed.
 */
import { computed, watch } from "vue";
import { useRouter, type RouteLocationRaw } from "vue-router";
import { useMutation, useQuery } from "@pinia/colada";
import { useFuse } from "@vueuse/integrations/useFuse";
import type { CommandPaletteGroup, CommandPaletteItem } from "@nuxt/ui";
import { GetEventDetail } from "@services/eventgraphservice";
import { Hover } from "@services/ideservice";
import { SWITCHES, ideSettingsOpen } from "../ide/IdeSettingsCard.vue";
import UnsavedChangesModal from "./UnsavedChangesModal.vue";
import { useCompare } from "../../composables/useCompare";
import { useIdeRoots } from "../../composables/useIdeRoots";
import { codesQuery, filesQuery, locQuery, rootsQuery, symbolsQuery, workspacesQuery } from "../../queries";
import { useGitStore } from "../../stores/git";
import { diffTab, useIdeStore } from "../../stores/ide";
import { useSettingsStore } from "../../stores/settings";
import { updatePrefs, useWorkspaceStore } from "../../stores/workspace";
import {
  PAGES,
  SETTINGS,
  SETTINGS_SECTIONS,
  WORKSPACE_TOOLS,
  workspaceHomeRoute,
  type SettingSwitch,
} from "../../workspaceTools";


const router = useRouter();
const store = useWorkspaceStore();
const settings = useSettingsStore();
const { mutate: savePrefs } = useMutation({ mutation: updatePrefs });
const { data: workspaces } = useQuery(workspacesQuery);
const { data: codes } = useQuery(codesQuery);
const ide = useIdeStore();
const unsavedModal = useOverlay().create(UnsavedChangesModal);
const git = useGitStore();
const { roots, locate } = useIdeRoots(() => store.activeWorkspaceId);
const compare = useCompare(() => store.activeWorkspaceId);

// the search, and what it looks through by its first character
const term = shallowRef("> ");
watch(paletteOpen, (open) => open && (term.value = start.value));
const mode = computed(() =>
  term.value.startsWith(">") ? "commands" : term.value.startsWith("@") ? "symbols" : term.value.startsWith("#") ? "events" : "files",
);
const query = computed(() => (mode.value === "files" ? term.value : term.value.slice(1)).trim());

type Item = CommandPaletteItem & { words?: string; scope?: string; on?: boolean; opens?: string };

function go(to: RouteLocationRaw): void {
  void router.push(to);
  paletteOpen.value = false;
}

/** Show a place of a file in the IDE (a 0-based line and UTF-8 byte column). */
function show(path: string, line = 0, col = 0): void {
  paletteOpen.value = false;
  ide.open(path, { line, col });
}

/** The IDE's editors, from their module, which loads with the IDE page. */
const editors = () => import("../../ide/monacoEditor");

async function graphAtCaret(): Promise<void> {
  const card = await Hover(store.activeWorkspaceId, ide.activeFile, ide.caret?.line ?? 0, ide.caret?.col ?? 0).catch(() => null);
  if (card?.graphed) go({ name: "event-graph", params: { id: store.activeWorkspaceId }, query: { root: card.word } });
}

/** A switch's state and how it flips, where it has a workspace to flip on. */
function flip(id: SettingSwitch): { on: boolean; set: (on: boolean) => void } | undefined {
  const ws = store.activeWorkspace;
  switch (id) {
    case "matchGame":
      return { on: settings.matchGameTheme, set: (on) => void settings.setOn("ui.matchGameTheme", on) };
    case "followSystem":
      return { on: settings.appearance === "system", set: (on) => void settings.followSystem(on) };
    case "reopenTabs":
      return ws && { on: !ws.resetIdeOnOpen, set: (on) => savePrefs({ ws, resetIdeOnOpen: !on }) };
    case "hideBinaries":
      return ws && { on: ws.hideExplorerBinaries, set: (on) => savePrefs({ ws, hideExplorerBinaries: on }) };
  }
}

function switchItem(label: string, scope: string, on: boolean, set: (on: boolean) => void, words = ""): Item {
  return {
    label,
    prefix: "Toggle:",
    icon: "i-lucide-check",
    slot: "switch",
    scope,
    on,
    words,
    onSelect: () => set(!on),
  };
}

const groups = computed<CommandPaletteGroup<Item>[]>(() => {
  const id = store.activeWorkspaceId;
  const settingsAt = (section: string) => ({ name: "workspace-settings" as const, params: { id }, query: { section } });
  const pages: Item[] = [
    { label: PAGES.library.title, icon: PAGES.library.icon, onSelect: () => go({ name: "library" }) },
    { label: "New workspace", icon: "i-lucide-plus", onSelect: () => go({ name: "wizard" }) },
    ...(store.activeWorkspace
      ? [
          ...WORKSPACE_TOOLS.filter((tool) => settings.visibleTools.includes(tool.name)).map((tool) => ({
            label: tool.label,
            icon: tool.icon,
            onSelect: () => go({ name: tool.name, params: { id } }),
          })),
          { label: PAGES["workspace-settings"].title, icon: PAGES["workspace-settings"].icon, onSelect: () => go(settingsAt("general")) },
        ]
      : []),
  ];
  const others: Item[] = (workspaces.value ?? []).map((ws) => ({
    label: ws.name,
    suffix: store.game(ws.gameId)?.shortName,
    icon: ws.id === id ? "i-lucide-check" : "i-lucide-folder",
    onSelect: () => go(workspaceHomeRoute(ws, settings.visibleTools)),
  }));
  const switches: Item[] = [
    ...SETTINGS.flatMap((setting) => {
      const state = setting.flips && flip(setting.flips);
      if (!state) return [];
      const scope = setting.section === "ide" ? "IDE" : SETTINGS_SECTIONS[setting.section].scope;
      return [switchItem(setting.label, scope, state.on, state.set, setting.words)];
    }),
    ...WORKSPACE_TOOLS.map((tool) =>
      switchItem(`Show the ${tool.label} page`, "App", settings.visibleTools.includes(tool.name), (on) => void settings.showTool(tool.name, on)),
    ),
    ...Object.values(SWITCHES)
      .flat()
      .map((row) => switchItem(row.label, row.key.startsWith("ui.wiki") ? "Wiki" : "IDE", settings.on(row.key), (on) => void settings.setOn(row.key, on), row.hint)),
    ...(codes.value ?? []).map((check) =>
      switchItem(`Check: ${check.summary}`, "IDE", settings.on(`ide.diag.${check.code}`, check.default), (on) =>
        void settings.setOn(`ide.diag.${check.code}`, on), check.code),
    ),
  ];
  // the IDE's commands, on the file it shows
  const active = ide.activeFile;
  const [first, second] = ide.selected;
  const picked = ide.comparing;
  const run = (fn: () => unknown) => () => {
    paletteOpen.value = false;
    void fn();
  };
  const compares: Item[] = [
    ...(active
      ? [
          { label: "Select the active file for compare", prefix: "Compare:", icon: "i-lucide-diff", onSelect: run(() => (ide.comparing = active)) },
          ...(picked && picked !== active
            ? [{ label: "Active file with the file selected", prefix: "Compare:", icon: "i-lucide-diff", onSelect: run(() => compare.files(picked, active)) }]
            : []),
          ...(git.repoOf(active)
            ? [{ label: "Active file with the last commit", prefix: "Compare:", icon: "i-lucide-git-compare", onSelect: run(() => ide.open(diffTab("changes", active))) }]
            : []),
          {
            label: "Active file with the game's copy",
            prefix: "Compare:",
            icon: "i-lucide-diff",
            disabled: !!locate(active)?.root.readOnly,
            onSelect: run(() => compare.withGame(active)),
          },
        ]
      : []),
    ...(first && second
      ? [{ label: "Two files selected in the explorer", prefix: "Compare:", icon: "i-lucide-diff", onSelect: run(() => compare.files(first, second)) }]
      : []),
  ];
  // the window loaded again, its unsaved files saved or dropped first
  const reload: Item = {
    label: "Reload the window",
    icon: "i-lucide-refresh-cw",
    onSelect: async () => {
      if (!ide.unsaved.size || (await unsavedModal.open({ going: "Reloading the window" }))) location.reload();
    },
  };
  const commands: Item[] = !id
    ? [reload]
    : [
        reload,
        ...compares,
        ...(active
          ? [
              { label: "Open the event under the cursor", prefix: "Event Graph:", icon: "i-lucide-git-branch", kbds: ["Ctrl Shift G"], onSelect: run(graphAtCaret) },
              { label: "Format document", prefix: "IDE:", icon: "i-lucide-align-left", kbds: ["Shift Alt F"], onSelect: run(async () => (await editors()).runAction("editor.action.formatDocument")) },
              { label: "Save", prefix: "IDE:", icon: "i-lucide-save", kbds: ["Ctrl S"], onSelect: run(async () => (await editors()).saveFile(active)) },
              { label: "Save all", prefix: "IDE:", icon: "i-lucide-save-all", onSelect: run(async () => (await editors()).saveAll()) },
              { label: "Split right", prefix: "IDE:", icon: "i-lucide-columns-2", kbds: ["Ctrl \\"], onSelect: run(() => ide.split("right")) },
              { label: "Split down", prefix: "IDE:", icon: "i-lucide-rows-2", onSelect: run(() => ide.split("down")) },
              { label: "Move the editor into the next group", prefix: "IDE:", icon: "i-lucide-arrow-right-to-line", onSelect: run(() => ide.moveToNext()) },
            ]
          : []),
        {
          label: "Find in files",
          prefix: "IDE:",
          icon: "i-lucide-search",
          kbds: ["Ctrl Shift F"],
          onSelect: () => {
            ide.sidebar = "search";
            go({ name: "workspace-ide", params: { id } });
          },
        },
        {
          label: ide.wikiOpen ? "Hide the Wiki pane" : "Show the Wiki pane",
          prefix: "IDE:",
          icon: "i-lucide-book-open",
          kbds: ["Ctrl Shift W"],
          onSelect: run(() => (ide.wikiOpen = !ide.wikiOpen)),
        },
      ];
  // a value opens where it is set: the IDE's settings card, or the settings page at its section
  const values: Item[] = !id ? [] : SETTINGS.filter((setting) => !setting.flips).map((setting) => {
    const section = setting.section === "ide" ? undefined : SETTINGS_SECTIONS[setting.section];
    return {
      label: setting.label,
      words: setting.words,
      icon: "i-lucide-settings",
      slot: "value",
      opens: !section ? "opens IDE settings" : section.scope === "App" ? `opens ${section.label}` : "opens workspace settings",
      onSelect: () => {
        if (section) return go(settingsAt(setting.section));
        go({ name: "workspace-ide", params: { id } });
        ideSettingsOpen.value = true;
      },
    };
  });
  return [
    { id: "commands", label: "Commands", items: commands },
    { id: "pages", label: "Pages", items: pages },
    { id: "workspaces", label: "Workspaces", items: others },
    { id: "switches", label: "Settings you can switch", items: switches },
    { id: "values", label: "Settings with a value", items: values },
  ];
});

// the commands as the search after ">" finds them, in their groups' order
const all = computed(() => groups.value.flatMap((group) => (group.items ?? []).map((item) => ({ ...item, group: group.id }))));
const { results } = useFuse(query, all, {
  fuseOptions: { keys: ["label", "prefix", "suffix", "words"], threshold: 0.4 },
  matchAllWhenSearchEmpty: true,
  resultLimit: 40,
});
const found = computed(() =>
  groups.value.map((group) => ({ ...group, ignoreFilter: true, items: results.value.map((hit) => hit.item).filter((item) => item.group === group.id) })),
);

// the other searches' finds, read from Go as the search changes
const id = computed(() => store.activeWorkspaceId);
const { data: files } = useQuery(() =>
  filesQuery({ id: id.value, pattern: mode.value === "files" ? query.value : "", folders: roots.value.map((root) => root.path) }),
);
const { data: symbols } = useQuery(() => symbolsQuery({ id: id.value, path: mode.value === "symbols" ? ide.activeFile : "" }));
const { data: events } = useQuery(() => rootsQuery({ id: id.value, search: query.value, live: mode.value === "events" && !!query.value }));
const { data: locs } = useQuery(() => locQuery({ id: id.value, prefix: mode.value === "events" ? query.value : "" }));
const lists = computed((): CommandPaletteGroup<Item>[] => {
  switch (mode.value) {
    case "commands":
      return found.value;
    case "files": {
      const items = (files.value?.hits ?? []).map((hit) => {
        const at = locate(hit.path);
        return { label: at?.rel ?? hit.path, suffix: at ? store.originName(at.root.origin ?? "") : "", icon: "i-lucide-file", onSelect: () => show(hit.path) };
      });
      return [{ id: "files", label: "Files", ignoreFilter: true, items }];
    }
    case "symbols": {
      const needle = query.value.toLowerCase();
      const items = (symbols.value ?? [])
        .filter((symbol) => symbol.name.toLowerCase().includes(needle))
        .slice(0, 100)
        .map((symbol) => ({
          label: symbol.name,
          suffix: symbol.database,
          icon: "i-lucide-hash",
          onSelect: () => show(symbol.location.path, symbol.location.range.start.line, symbol.location.range.start.character),
        }));
      return [{ id: "symbols", label: "In this file", ignoreFilter: true, items }];
    }
    case "events": {
      const items = (events.value?.hits ?? []).map((node) => ({
        label: node.id,
        suffix: `${node.kind} · ${store.originName(node.origin ?? "")}`,
        icon: "i-lucide-zap",
        onSelect: async () => {
          const detail = await GetEventDetail(id.value, node.id).catch(() => null);
          if (detail?.at.path) show(detail.at.path, detail.line);
        },
      }));
      const keys = (locs.value ?? []).map((found) => ({
        label: found.name,
        suffix: store.originName(found.location.origin),
        icon: "i-lucide-languages",
        onSelect: () => show(found.location.path, found.location.range.start.line),
      }));
      return [
        { id: "events", label: "Events and on_actions", ignoreFilter: true, items },
        { id: "loc", label: "Localization", ignoreFilter: true, items: keys },
      ];
    }
  }
});
</script>

<template>
  <UModal v-model:open="paletteOpen" :ui="{ content: 'top-14 translate-y-0 sm:max-w-160' }">
    <template #content>
      <UCommandPalette
        v-model:search-term="term"
        :groups="lists"
        placeholder="Search files, or > for commands and settings"
        :close="true"
        class="max-h-120"
        @update:open="paletteOpen = false"
      >
        <template #close>
          <UKbd value="Esc" />
        </template>
        <template #switch-trailing="{ item }">
          <UBadge :label="item.scope" size="sm" />
          <USwitch :model-value="item.on" size="sm" class="pointer-events-none" />
        </template>
        <template #value-trailing="{ item }">
          <span class="text-11 text-dimmed">{{ item.opens }}</span>
        </template>
        <template #footer>
          <p class="text-11 text-dimmed">
            {{ mode === "commands" ? "Delete the > to open files." : "> for commands and settings." }}
            @ finds names in this file, # finds events, on_actions and localization keys. Enter flips a switch where you are.
          </p>
        </template>
      </UCommandPalette>
    </template>
  </UModal>
</template>
