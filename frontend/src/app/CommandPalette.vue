<!-- The command palette. It searches files, definitions and commands, or lists choices a command hands it. -->
<script setup lang="ts">
import type { CommandPaletteGroup, CommandPaletteItem } from "@nuxt/ui";
import { useQuery } from "@pinia/colada";
import { useEventBus, useLocalStorage } from "@vueuse/core";
import { useFuse } from "@vueuse/integrations/useFuse";
import { computed, nextTick, shallowRef } from "vue";

import type { WorkspaceSymbol } from "@services/lsp/models";
import type { Place } from "@services/workspace/models";
import { toSentence } from "../lib/format";
import { codesQuery, fileSearchQuery, settingsCatalogQuery, symbolsQuery, workspacesQuery } from "../queries";
import { useEditorStore } from "../stores/editor";
import { useSettingsStore } from "../stores/settings";
import { COMMAND_BUS } from "../stores/windows";
import { useWorkspaceStore } from "../stores/workspace";
import { COMMANDS, findCommand, listBindings, openPage } from "./commands";
import { homeRoute, listShownTools, PAGES, WORKSPACE_TOOLS, type Page, type PageName } from "./pages";
import { router } from "./router";

const open = defineModel<boolean>("open", { default: false });

/** How many files the palette lists for a search. It is also the most rows the palette shows in a group. */
const FILE_HITS = 50;
const RECENT = 5;
// The settings that hold a list. The palette switches each item on a row of its own.
const LISTS = ["appearance.pages", "ide.checks"];

const editor = useEditorStore();
const settings = useSettingsStore();
const workspace = useWorkspaceStore();
const commandBus = useEventBus(COMMAND_BUS);
const term = shallowRef("");
const choices = shallowRef<CommandPaletteGroup | null>(null);
const recent = useLocalStorage<string[]>("pmt.palette.recent", []);

// What the search looks through: > for commands, @ for definitions, # for localization keys, anything else for files.
const mode = computed(() => {
  if (choices.value) return "choices";
  if (term.value.startsWith(">")) return "commands";
  return /^[@#]/.test(term.value) ? "symbols" : "files";
});
const query = computed(() => (mode.value === "files" ? term.value : term.value.slice(1)).trim());
// # lists localization keys, @ definitions.
const localization = computed(() => term.value.startsWith("#"));

const { data: workspaces } = useQuery(workspacesQuery);
const { data: catalog } = useQuery(settingsCatalogQuery);
const { data: codes } = useQuery(codesQuery);
const { data: files } = useQuery(() => ({
  ...fileSearchQuery({ query: query.value, limit: FILE_HITS }),
  enabled: open.value && mode.value === "files" && workspace.isOpen(),
  placeholderData: (previous: Place[] | null | undefined) => previous,
}));
const { data: symbols } = useQuery(() => ({
  ...symbolsQuery({ query: query.value, localization: localization.value }),
  enabled: open.value && mode.value === "symbols" && !!query.value && workspace.isOpen(),
  placeholderData: (previous: WorkspaceSymbol[] | null | undefined) => previous,
}));

const available = computed((): { id: string; label: string; items: CommandPaletteItem[] }[] => {
  if (mode.value !== "commands") return [];
  const activeId = workspace.activeId();
  const applicable = COMMANDS.filter((row) => row.when()).map((row) => ({ ...row, kbds: listBindings(row)[0] }));
  const recentRows = recent.value.flatMap((id) => applicable.find((row) => row.id === id) ?? []);
  const pageNames: PageName[] = ["library", "new-mod", "wizard", ...(activeId ? listShownTools() : [])];
  const pages = pageNames.map((name) => {
    const page: Page = PAGES[name];
    return {
      id: `page.${name}`,
      label: page.title,
      icon: page.icon ?? "i-lucide-plus",
      onSelect: () => openPage(name),
    };
  });
  const records = (workspaces.value ?? []).map(({ record }) => ({
    id: `workspace.${record.id}`,
    label: record.name,
    icon: record.id === activeId ? "i-lucide-check" : "i-lucide-folder",
    onSelect: () => void router.push(homeRoute(record.id, record.homePage)),
  }));
  return [
    { id: "recent", label: "Recently used", items: recentRows },
    { id: "commands", label: "Commands", items: applicable.filter((row) => !recentRows.includes(row)) },
    { id: "pages", label: "Pages", items: pages },
    { id: "workspaces", label: "Workspaces", items: records },
    { id: "switches", label: "Settings you can switch", items: listSwitches() },
    { id: "values", label: "Settings with a value", items: listValues() },
  ];
});
const { results } = useFuse(
  query,
  () => available.value.flatMap((group) => group.items.map((item) => ({ ...item, group: group.id }))),
  {
    fuseOptions: { keys: [{ name: "label", weight: 3 }, "prefix", "suffix", "words"], threshold: 0.3 },
    matchAllWhenSearchEmpty: true,
    resultLimit: 40,
  },
);

const groups = computed<CommandPaletteGroup[]>(() => {
  if (choices.value) return [{ id: choices.value.id, items: choices.value.items }];
  if (mode.value === "files") {
    const items = (files.value ?? []).map((place) => ({
      label: place.rel,
      suffix: workspace.originName(place.origin),
      icon: "i-lucide-file",
      onSelect: () => editor.show(place),
    }));
    return [{ id: "files", label: "Files", ignoreFilter: true, items }];
  }
  if (mode.value === "symbols") {
    const items = (symbols.value ?? []).map(({ name, database, site }) => ({
      label: name,
      suffix: database,
      icon: "i-lucide-hash",
      onSelect: () =>
        editor.show(site.place, { lineNumber: site.range.startLineNumber, column: site.range.startColumn }),
    }));
    const label = localization.value ? "Localization keys" : "Definitions";
    return [{ id: "symbols", label, ignoreFilter: true, items }];
  }
  // The group of the best match comes first.
  const hits = results.value.map((hit) => hit.item);
  return [...new Set(hits.map((item) => item.group))].map((id) => ({
    id,
    label: available.value.find((group) => group.id === id)?.label,
    ignoreFilter: true,
    items: hits.filter((item) => item.group === id),
  }));
});

commandBus.on((command) => {
  if (command !== "palette.open" && command !== "editor.goToFile") return;
  choices.value = null;
  term.value = command === "palette.open" ? "> " : "";
  open.value = true;
});
useEventBus<CommandPaletteGroup>("pmt.choices").on((group) => {
  choices.value = group;
  term.value = "";
  open.value = true;
});

/** listSwitches lists the palette's switches: settings, pages and checks. It leaves out what would show nothing. */
function listSwitches(): CommandPaletteItem[] {
  const settingRows = catalog.value ?? [];
  const findSetting = (key: string) => settingRows.find((setting) => setting.key === key);
  const isOpen = !!workspace.activeId();
  const switches = settingRows
    .filter((setting) => setting.type === "switch" && (isOpen || !setting.workspace) && !settings.isHidden(setting.key))
    .map((setting) =>
      makeToggle(
        `setting.${setting.key}`,
        { label: setting.label, words: setting.words, scope: setting.workspace ? "IDE" : "App" },
        settings.isOn(setting.key),
        (on) => settings.write(setting.key, String(on)),
      ),
    );
  const shown = listShownTools();
  const pages = WORKSPACE_TOOLS.map((tool) =>
    makeToggle(
      `setting.appearance.pages.${tool}`,
      { label: `Show the ${PAGES[tool].title} page`, words: findSetting("appearance.pages")?.words, scope: "App" },
      shown.includes(tool),
      (on) => settings.writeItem("appearance.pages", tool, on),
    ),
  );
  // The setting lists the checks turned off.
  const off = settings.value("ide.checks").split(",");
  const checks = (isOpen ? (codes.value ?? []) : []).map((code) =>
    makeToggle(
      `setting.ide.checks.${code.id}`,
      { label: `Check: ${code.label}`, words: [code.id, findSetting("ide.checks")?.words].join(" "), scope: "IDE" },
      !off.includes(code.id),
      (on) => settings.writeItem("ide.checks", code.id, !on),
    ),
  );
  return [...switches, ...pages, ...checks];
}

/** makeToggle makes a row of the palette that flips a setting. */
function makeToggle(
  id: string,
  row: CommandPaletteItem,
  on: boolean,
  write: (on: boolean) => void,
): CommandPaletteItem {
  return { ...row, id, prefix: "Toggle:", icon: "i-lucide-check", slot: "setting", on, onSelect: () => write(!on) };
}

/** listValues lists the palette's settings with a value. Each opens Settings at its row. */
function listValues(): CommandPaletteItem[] {
  const isOpen = !!workspace.activeId();
  return (catalog.value ?? [])
    .filter((setting) => setting.type !== "switch" && !LISTS.includes(setting.key) && (isOpen || !setting.workspace))
    .map((setting) => ({
      id: `setting.${setting.key}`,
      label: setting.label,
      words: setting.words,
      icon: "i-lucide-settings",
      slot: "setting",
      opens: `opens ${toSentence(setting.section)}`,
      onSelect: () => commandBus.emit("settings.open", setting.key),
    }));
}

/** pick closes the palette after a row is picked, unless a command just handed it a list. */
function pick(row: CommandPaletteItem): void {
  if (typeof row.id === "string" && findCommand(row.id)) {
    recent.value = [row.id, ...recent.value.filter((id) => id !== row.id)].slice(0, RECENT);
  }
  if (!choices.value || row.id !== choices.value.id) open.value = false;
}

/** goBack returns from a command's list to the commands on Backspace in an empty search. */
function goBack(event: KeyboardEvent): void {
  if (!choices.value || term.value) return;
  event.preventDefault();
  choices.value = null;
  term.value = "> ";
  void nextTick(() => placeCaret(event.target as HTMLInputElement));
}

/** focusSearch focuses the search box as the palette opens, with the caret after its text and nothing selected. */
function focusSearch(event: Event): void {
  event.preventDefault();
  const input = (event.target as HTMLElement).querySelector("input");
  input?.focus();
  if (input) placeCaret(input);
}

/** placeCaret puts the caret of a search box after its text. */
function placeCaret(input: HTMLInputElement): void {
  input.setSelectionRange(input.value.length, input.value.length);
}
</script>

<template>
  <UModal
    v-model:open="open"
    :content="{ onOpenAutoFocus: focusSearch }"
    :ui="{ content: 'top-14 translate-y-0 sm:max-w-160' }"
  >
    <template #content>
      <!-- The search box's keys pass through here on their way up. -->
      <div class="contents" @keydown.backspace="goBack">
        <UCommandPalette
          v-model:search-term="term"
          :groups="groups"
          :fuse="{ resultLimit: FILE_HITS }"
          :placeholder="choices?.label ?? 'Search files, or > for commands and settings'"
          :close="true"
          class="max-h-120"
          @update:open="open = false"
          @update:model-value="pick"
        >
          <template #close>
            <UKbd value="Esc" />
          </template>
          <template #setting-trailing="{ item }">
            <template v-if="item.on !== undefined">
              <UBadge :label="item.scope" size="sm" class="uppercase" />
              <USwitch :model-value="item.on" size="sm" class="pointer-events-none" />
            </template>
            <span v-else class="text-11 text-dimmed">{{ item.opens }}</span>
          </template>
          <template #footer>
            <p v-if="choices" class="text-11 text-dimmed">
              Enter picks. Backspace in an empty search goes back to the commands.
            </p>
            <p v-else class="text-11 text-dimmed">
              {{ mode === "commands" ? "Delete the > to open files." : "> for commands and settings." }}
              @ finds definitions and # finds localization keys. Enter flips a switch where you are.
            </p>
          </template>
        </UCommandPalette>
      </div>
    </template>
  </UModal>
</template>
