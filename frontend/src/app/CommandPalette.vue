<!--
  The command palette. It lists the commands by group, the ones run last first, and runs the one picked. A command
  can also hand it a list to pick one from.
-->
<script setup lang="ts">
import type { CommandPaletteGroup, CommandPaletteItem } from "@nuxt/ui";
import { useQuery } from "@pinia/colada";
import { useEventBus } from "@vueuse/core";
import { useFuse } from "@vueuse/integrations/useFuse";
import { computed, shallowRef } from "vue";

import type { IPosition } from "monaco-editor";
import type { WorkspaceSymbol } from "@services/lsp/models";
import type { Place } from "@services/workspace/models";
import { codesQuery, fileSearchQuery, settingsCatalogQuery, symbolsQuery, workspacesQuery } from "../queries";
import { useEditorStore } from "../stores/editor";
import { useWorkspaceStore } from "../stores/workspace";
import { paletteGroups, rememberCommand } from "./commands";

const open = defineModel<boolean>("open", { default: false });

/** How many files the palette lists for a search. */
const FILE_HITS = 50;

const editor = useEditorStore();
const workspace = useWorkspaceStore();
const term = shallowRef("");
// the list a command handed the palette to pick from, under the command's id, or null when there is none
const choices = shallowRef<CommandPaletteGroup | null>(null);

// what the search looks through: a command's list while one is shown, else by the search's first character: ">"
// the commands, "@" or "#" the definitions, else the files
const mode = computed(() => {
  if (choices.value) return "choices";
  if (term.value.startsWith(">")) return "commands";
  return /^[@#]/.test(term.value) ? "symbols" : "files";
});
const query = computed(() => (mode.value === "files" ? term.value : term.value.slice(1)).trim());

// the commands' lists read these from the cache, so they are kept loaded while the palette exists
useQuery(workspacesQuery);
useQuery(settingsCatalogQuery);
useQuery(codesQuery);
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

const available = computed(() => (mode.value === "commands" ? paletteGroups() : []));
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
      onSelect: () => show(place),
    }));
    return [{ id: "files", label: "Files", ignoreFilter: true, items }];
  }
  if (mode.value === "symbols") {
    const items = (symbols.value ?? []).map(({ name, database, location }) => ({
      label: name,
      suffix: database,
      icon: "i-lucide-hash",
      onSelect: () =>
        show(location.place, { lineNumber: location.range.startLineNumber, column: location.range.startColumn }),
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

useEventBus<string>("pmt.command").on((command) => {
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

/** Shows a place in the IDE. */
function show(place: Place, position?: IPosition): void {
  editor.show(place, position, "palette");
}

/**
 * Closes the palette once a row has run. A command that handed the palette a list under its own id leaves it open
 * on that list. A command is kept among the ones run last.
 */
function pick(row: CommandPaletteItem): void {
  if (typeof row.id === "string") rememberCommand(row.id);
  if (!choices.value || row.id !== choices.value.id) open.value = false;
}

/** Goes back from a command's list to the commands when Backspace is pressed on an empty search. */
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
