<!-- Where a mod's Workshop description comes from, in a sentence, with the menu that changes it at any time. -->
<script setup lang="ts">
import { computed } from "vue";
import { useMutation, useQuery } from "@pinia/colada";
import { Dialogs } from "@wailsio/runtime";
import type { DropdownMenuItem } from "@nuxt/ui";

import { listingQuery, stateQuery } from "../queries";
import { useWorkspaceStore } from "../stores/workspace";
import * as steam from "@services/steam/service";

/** Each source a description is kept in: its menu label, its sentence, and the files that may hold it, if any. */
const SOURCES = [
  {
    value: "markdown",
    label: "Markdown",
    sentence: "Markdown, converted to BBCode when you upload. Uploads replace Steam's text.",
    filter: "*.md",
  },
  {
    value: "bbcode",
    label: "BBCode",
    sentence: "BBCode, uploaded as written. Uploads replace Steam's text.",
    filter: "*.bbcode;*.txt",
  },
  {
    value: "steam",
    label: "Steam's own text",
    sentence: "Steam keeps its own text: uploads leave it as it is. Edit it on the Workshop page.",
  },
];

const { modId } = defineProps<{ modId: string }>();

const workspace = useWorkspaceStore();
const { data: state } = useQuery(stateQuery);
const { data: listing } = useQuery(() => ({ ...listingQuery(modId), enabled: workspace.isOpen() }));

const { mutate: choose, isLoading: choosing } = useMutation({
  mutation: ({ source, file }: { source: string; file: string }) => steam.SetDescriptionSource(modId, source, file),
});

const sentence = computed(() => SOURCES.find((row) => row.value === listing.value?.source)?.sentence ?? "");
// the three sources, then another file of the mod as Markdown or BBCode
const items = computed<DropdownMenuItem[]>(() => [
  ...SOURCES.map((row) => ({ label: row.label, onSelect: () => choose({ source: row.value, file: "" }) })),
  {
    label: "Another file in the mod",
    children: SOURCES.filter((row) => row.filter).map((row) => ({
      label: `As ${row.label}…`,
      onSelect: () => void pickFile(row.value, row.filter ?? ""),
    })),
  },
]);

/** pickFile asks for a file in the mod's folder and keeps the description in it, in the given source's format. */
async function pickFile(source: string, filter: string): Promise<void> {
  const file = await Dialogs.OpenFile({
    CanChooseFiles: true,
    Directory: state.value?.mods?.find((mod) => mod.id === modId)?.dir,
    Filters: [{ DisplayName: "Description", Pattern: filter }],
  });
  if (file) choose({ source, file });
}
</script>

<template>
  <p class="flex flex-wrap items-baseline gap-x-2 text-12 text-dimmed">
    {{ sentence }}
    <UDropdownMenu :items="items">
      <UButton label="Change source" variant="link" :loading="choosing" class="h-5 p-0 text-12" />
    </UDropdownMenu>
  </p>
</template>
