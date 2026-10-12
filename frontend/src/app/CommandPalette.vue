<script setup lang="ts">
/**
 * The command palette on every page. After ">", the pages, the
 * workspaces, the IDE's commands and every setting: a setting that is on or off flips where you are, one with a value
 * opens the page or the card that sets it. Without it, the workspace's files; after "@", the open file's names; after
 * "#", events and on_actions, the game's included, and the localization keys starting with what is typed.
 */
import { computed, shallowRef, watch } from "vue";
import { useRouter, type RouteLocationRaw } from "vue-router";
import { useMutation, useQuery } from "@pinia/colada";
import { useFuse } from "@vueuse/integrations/useFuse";
import type { CommandPaletteGroup } from "@nuxt/ui";
import { GetEventDetail } from "@services/eventgraphservice";
import UnsavedChangesModal from "../ui/UnsavedChangesModal.vue";
import { useIdeRoots } from "../composables/useIdeRoots";
import { codesQuery, filesQuery, locQuery, rootsQuery, symbolsQuery, workspacesQuery } from "../queries";
import { useCommandsStore, type CommandItem as Item } from "../stores/commands";
import { useEditorStore } from "../stores/editor";
import { IDE_SWITCH_ROWS, useSettingsStore } from "../stores/settings";
import { updatePrefs, useWorkspaceStore } from "../stores/workspace";
import { PAGES, SETTINGS, SETTINGS_SECTIONS, WORKSPACE_TOOLS, workspaceHomeRoute, type SettingSwitch } from "./pages";

const router = useRouter();
const store = useWorkspaceStore();
const settings = useSettingsStore();
const { mutate: savePrefs } = useMutation({ mutation: updatePrefs });
const { data: workspaces } = useQuery(workspacesQuery);
const { data: codes } = useQuery(codesQuery);
const editor = useEditorStore();
const palette = useCommandsStore();
const unsavedModal = useOverlay().create(UnsavedChangesModal);
const { roots, locate } = useIdeRoots(() => store.activeWorkspaceId);

// the search, and what it looks through by its first character
const term = shallowRef("> ");
watch(
  () => palette.paletteOpen,
  (open) => open && (term.value = palette.paletteStart),
);
const mode = computed(() =>
  term.value.startsWith(">")
    ? "commands"
    : term.value.startsWith("@")
      ? "symbols"
      : term.value.startsWith("#")
        ? "events"
        : "files",
);
const query = computed(() => (mode.value === "files" ? term.value : term.value.slice(1)).trim());

function go(target: RouteLocationRaw): void {
  void router.push(target);
  palette.paletteOpen = false;
}

/** Show a place of a file in the IDE (a 0-based line and UTF-8 byte column). */
function show(path: string, line = 0, col = 0): void {
  palette.paletteOpen = false;
  editor.open(path, { line, col });
}

async function reloadWindow(): Promise<void> {
  if (!editor.unsaved.size || (await unsavedModal.open({ going: "Reloading the window" }))) location.reload();
}

/** A switch's state and how it flips, where it has a workspace to flip on. */
function flip(id: SettingSwitch): { on: boolean; set: (on: boolean) => void } | undefined {
  const ws = store.activeWorkspace;
  switch (id) {
    case "matchGame":
      return { on: settings.matchGameTheme, set: (on) => settings.setOn("ui.matchGameTheme", on) };
    case "followSystem":
      return { on: settings.appearance === "system", set: (on) => settings.followSystem(on) };
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
          {
            label: PAGES["workspace-settings"].title,
            icon: PAGES["workspace-settings"].icon,
            onSelect: () => go(settingsAt("general")),
          },
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
      switchItem(`Show the ${tool.label} page`, "App", settings.visibleTools.includes(tool.name), (on) =>
        settings.showTool(tool.name, on),
      ),
    ),
    ...IDE_SWITCH_ROWS.map((row) =>
      switchItem(
        row.label,
        row.key.startsWith("ui.wiki") ? "Wiki" : "IDE",
        settings.on(row.key),
        (on) => settings.setOn(row.key, on),
        row.hint,
      ),
    ),
    ...(codes.value ?? []).map((check) =>
      switchItem(
        `Check: ${check.summary}`,
        "IDE",
        settings.on(`ide.diag.${check.code}`, check.default),
        (on) => settings.setOn(`ide.diag.${check.code}`, on),
        check.code,
      ),
    ),
  ];
  // the window loaded again, its unsaved files saved or dropped first
  const reload: Item = {
    label: "Reload the window",
    icon: "i-lucide-refresh-cw",
    onSelect: () => void reloadWindow(),
  };
  // the pages' own commands, which they register, follow it
  const commands: Item[] = [reload, ...(id ? palette.commands : [])];
  // a value opens where it is set: the IDE's settings card, or the settings page at its section
  const values: Item[] = !id
    ? []
    : SETTINGS.filter((setting) => !setting.flips).map((setting) => {
        const section = setting.section === "ide" ? undefined : SETTINGS_SECTIONS[setting.section];
        return {
          label: setting.label,
          words: setting.words,
          icon: "i-lucide-settings",
          slot: "value",
          opens: !section
            ? "opens IDE settings"
            : section.scope === "App"
              ? `opens ${section.label}`
              : "opens workspace settings",
          onSelect: () => {
            if (section) return go(settingsAt(setting.section));
            go({ name: "workspace-ide", params: { id } });
            editor.ideSettingsOpen = true;
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
const all = computed(() =>
  groups.value.flatMap((group) => (group.items ?? []).map((item) => ({ ...item, group: group.id }))),
);
const { results } = useFuse(query, all, {
  fuseOptions: { keys: ["label", "prefix", "suffix", "words"], threshold: 0.4 },
  matchAllWhenSearchEmpty: true,
  resultLimit: 40,
});
const commandGroups = computed(() =>
  groups.value.map((group) => ({
    ...group,
    ignoreFilter: true,
    items: results.value.map((hit) => hit.item).filter((item) => item.group === group.id),
  })),
);

// the other searches' finds, read from Go as the search changes
const id = computed(() => store.activeWorkspaceId);
const { data: files } = useQuery(() =>
  filesQuery({
    id: id.value,
    pattern: mode.value === "files" ? query.value : "",
    folders: roots.value.map((root) => root.path),
  }),
);
const { data: symbols } = useQuery(() =>
  symbolsQuery({ id: id.value, path: mode.value === "symbols" ? editor.activeFile : "" }),
);
const { data: events } = useQuery(() =>
  rootsQuery({ id: id.value, search: query.value, live: mode.value === "events" && !!query.value }),
);
const { data: locs } = useQuery(() => locQuery({ id: id.value, prefix: mode.value === "events" ? query.value : "" }));
const lists = computed((): CommandPaletteGroup<Item>[] => {
  switch (mode.value) {
    case "commands":
      return commandGroups.value;
    case "files": {
      const items = (files.value?.hits ?? []).map((hit) => {
        const located = locate(hit.path);
        return {
          label: located?.rel ?? hit.path,
          suffix: located ? store.originName(located.root.origin ?? "") : "",
          icon: "i-lucide-file",
          onSelect: () => show(hit.path),
        };
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
          onSelect: () =>
            show(symbol.location.path, symbol.location.range.start.line, symbol.location.range.start.character),
        }));
      return [{ id: "symbols", label: "In this file", ignoreFilter: true, items }];
    }
    case "events": {
      const items = (events.value?.hits ?? []).map((node) => ({
        label: node.id,
        suffix: `${node.kind} · ${store.originName(node.origin ?? "")}`,
        icon: "i-lucide-zap",
        onSelect: async () => {
          const detail = await GetEventDetail(id.value, node.id);
          if (detail?.at.path) show(detail.at.path, detail.line);
        },
      }));
      const keys = (locs.value ?? []).map((entry) => ({
        label: entry.name,
        suffix: store.originName(entry.location.origin),
        icon: "i-lucide-languages",
        onSelect: () => show(entry.location.path, entry.location.range.start.line),
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
  <UModal v-model:open="palette.paletteOpen" :ui="{ content: 'top-14 translate-y-0 sm:max-w-160' }">
    <template #content>
      <UCommandPalette
        v-model:search-term="term"
        :groups="lists"
        placeholder="Search files, or > for commands and settings"
        :close="true"
        class="max-h-120"
        @update:open="palette.paletteOpen = false"
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
            @ finds names in this file, # finds events, on_actions and localization keys. Enter flips a switch where you
            are.
          </p>
        </template>
      </UCommandPalette>
    </template>
  </UModal>
</template>
