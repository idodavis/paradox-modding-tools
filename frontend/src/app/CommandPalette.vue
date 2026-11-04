<!--
  The command palette. It lists the commands by group, the ones run last first, and runs the one picked. A command
  can also hand it a list to pick one from.
-->
<script setup lang="ts">
import type { CommandPaletteGroup, CommandPaletteItem } from "@nuxt/ui";
import { useQuery } from "@pinia/colada";
import { useEventBus, useLocalStorage } from "@vueuse/core";
import { useFuse } from "@vueuse/integrations/useFuse";
import { computed, shallowRef } from "vue";

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

/** How many files the palette lists for a search. */
const FILE_HITS = 50;
// how many commands the palette's Recently used lists
const RECENT = 5;
// the settings that hold a list, each item of which the palette switches on a row of its own
const LISTS = ["appearance.pages", "ide.checks"];

const editor = useEditorStore();
const settings = useSettingsStore();
const workspace = useWorkspaceStore();
const commandBus = useEventBus(COMMAND_BUS);
const term = shallowRef("");
// the list a command handed the palette to pick from, under the command's id, or null when there is none
const choices = shallowRef<CommandPaletteGroup | null>(null);
// the ids of the commands the palette ran last, the latest first
const recent = useLocalStorage<string[]>("pmt.palette.recent", []);

// What the search looks through: a command's list while one is shown, else by the first character of the search: > for
// commands, @ or # for definitions, anything else for files.
const mode = computed(() => {
  if (choices.value) return "choices";
  if (term.value.startsWith(">")) return "commands";
  return /^[@#]/.test(term.value) ? "symbols" : "files";
});
const query = computed(() => (mode.value === "files" ? term.value : term.value.slice(1)).trim());

const { data: workspaces } = useQuery(workspacesQuery);
const { data: catalog } = useQuery(settingsCatalogQuery);
const { data: codes } = useQuery(codesQuery);
const { data: files } = useQuery(() => ({
  ...fileSearchQuery({ query: query.value, limit: FILE_HITS }),
  enabled: open.value && mode.value === "files" && workspace.isOpen(),
  placeholderData: (previous: Place[] | null | undefined) => previous,
}));
const { data: symbols } = useQuery(() => ({
  ...symbolsQuery(query.value),
  enabled: open.value && mode.value === "symbols" && !!query.value && workspace.isOpen(),
  placeholderData: (previous: WorkspaceSymbol[] | null | undefined) => previous,
}));

const available = computed((): { id: string; label: string; items: CommandPaletteItem[] }[] => {
  if (mode.value !== "commands") return [];
  const activeId = workspace.activeId();
  // each row shows its first shortcut as the user set it
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
    fuseOptions: { keys: ["label", "prefix", "suffix", "words"], threshold: 0.4 },
    matchAllWhenSearchEmpty: true,
    resultLimit: 40,
  },
);

const groups = computed<CommandPaletteGroup[]>(() => {
  // the palette's own filter narrows a command's list
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
    return [{ id: "symbols", label: "Definitions", ignoreFilter: true, items }];
  }
  return available.value.map((group) => ({
    id: group.id,
    label: group.label,
    ignoreFilter: true,
    items: results.value.filter((hit) => hit.item.group === group.id).map((hit) => hit.item),
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

/**
 * listSwitches lists the palette's switches: each setting that is one, each page a workspace can show and each of
 * the checks. The IDE's settings and the checks are left out while no workspace is open, and so is a setting the
 * simple interface hides, since flipping it would show nothing.
 */
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
  // the setting lists the checks turned off
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

/**
 * makeToggle makes a row of the palette that flips a setting. It shows whether the setting is on, and picking it
 * flips it.
 */
function makeToggle(
  id: string,
  row: CommandPaletteItem,
  on: boolean,
  write: (on: boolean) => void,
): CommandPaletteItem {
  return { ...row, id, prefix: "Toggle:", icon: "i-lucide-check", slot: "setting", on, onSelect: () => write(!on) };
}

/**
 * listValues lists the palette's settings with a value. Each opens Settings at its row. The IDE's settings are left out
 * while no workspace is open.
 */
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

/**
 * pick closes the palette after a row is picked, except for a command that just handed the palette a list: the palette
 * stays open on that list. A command is added to the recently used ones; other rows, such as a file, are not.
 */
function pick(row: CommandPaletteItem): void {
  if (typeof row.id === "string" && findCommand(row.id)) {
    recent.value = [row.id, ...recent.value.filter((id) => id !== row.id)].slice(0, RECENT);
  }
  if (!choices.value || row.id !== choices.value.id) open.value = false;
}

/** goBack goes back from a command's list to the commands when Backspace is pressed on an empty search. */
function goBack(event: KeyboardEvent): void {
  if (!choices.value || term.value) return;
  event.preventDefault();
  choices.value = null;
  term.value = "> ";
}
</script>

<template>
  <UModal v-model:open="open" :ui="{ content: 'top-14 translate-y-0 sm:max-w-160' }">
    <template #content>
      <!-- the search box's keys pass through here on their way up -->
      <div class="contents" @keydown.backspace="goBack">
        <UCommandPalette
          v-model:search-term="term"
          :groups="groups"
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
              @ or # finds definitions and localization keys. Enter flips a switch where you are.
            </p>
          </template>
        </UCommandPalette>
      </div>
    </template>
  </UModal>
</template>
