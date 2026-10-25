<!-- The command palette. It lists the commands by group and runs the one picked. -->
<script setup lang="ts">
import type { CommandPaletteGroup } from "@nuxt/ui";
import { useQuery } from "@pinia/colada";
import { useEventBus } from "@vueuse/core";
import { useFuse } from "@vueuse/integrations/useFuse";
import { computed, shallowRef } from "vue";

import type { IPosition } from "monaco-editor";
import type { WorkspaceSymbol } from "@services/lsp/models";
import type { Place } from "@services/workspace/models";
import { fileSearchQuery, settingsCatalogQuery, symbolsQuery, workspacesQuery } from "../queries";
import { useEditorStore } from "../stores/editor";
import { useWorkspaceStore } from "../stores/workspace";
import { paletteGroups } from "./commands";

const open = defineModel<boolean>("open", { default: false });

const editor = useEditorStore();
const workspace = useWorkspaceStore();
const term = shallowRef("");

// what the search looks through by its first character: ">" the commands, "@" or "#" the definitions, else the files
const mode = computed(() => {
  if (term.value.startsWith(">")) return "commands";
  return /^[@#]/.test(term.value) ? "symbols" : "files";
});
const query = computed(() => (mode.value === "files" ? term.value : term.value.slice(1)).trim());

// the commands' lists read these from the cache, so they are kept loaded while the palette exists
useQuery(workspacesQuery);
useQuery(settingsCatalogQuery);
const { data: files } = useQuery(() => ({
  ...fileSearchQuery(query.value),
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
  term.value = command === "palette.open" ? "> " : "";
  open.value = true;
});

/** Shows a place in the IDE. */
function show(place: Place, position?: IPosition): void {
  editor.show(place, position, "palette");
}
</script>

<template>
  <UModal v-model:open="open" :ui="{ content: 'top-14 translate-y-0 sm:max-w-160' }">
    <template #content>
      <UCommandPalette
        v-model:search-term="term"
        :groups="groups"
        placeholder="Search files, or > for commands and settings"
        :close="true"
        class="max-h-120"
        @update:open="open = false"
        @update:model-value="open = false"
      >
        <template #close>
          <UKbd value="Esc" />
        </template>
        <template #setting-trailing="{ item }">
          <UBadge :label="item.scope" size="sm" class="uppercase" />
          <USwitch v-if="item.on !== undefined" :model-value="item.on" size="sm" class="pointer-events-none" />
          <span v-else class="text-11 text-dimmed">opens Settings</span>
        </template>
        <template #footer>
          <p class="text-11 text-dimmed">
            {{ mode === "commands" ? "Delete the > to open files." : "> for commands and settings." }}
            @ or # finds definitions and localization keys. Enter flips a switch where you are.
          </p>
        </template>
      </UCommandPalette>
    </template>
  </UModal>
</template>
