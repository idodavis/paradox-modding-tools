<!--
  The next upload beside the last one: the listing's fields and the files an upload changes, each marked added,
  changed or removed, and the one picked drawn as Steam has it beside the workspace's. Steam's side is the copy PMT
  kept at the last upload, or Steam's own once downloaded.
-->
<script setup lang="ts">
import { computed, shallowRef } from "vue";
import { useQuery } from "@pinia/colada";

import { day, plural, steamSentences } from "../../lib/format";
import { comparedFileQuery } from "../../queries";
import CompareView from "../../ui/CompareView.vue";
import type { Comparison, Listing } from "@services/steam/models";

const { modId, listing, comparison } = defineProps<{ modId: string; listing: Listing; comparison: Comparison }>();

/** Each listing field Go compares, with its name and its text as a compare shows it. */
const FIELDS: Record<string, { label: string; text: (one: Listing) => string }> = {
  title: { label: "Name", text: (one) => one.title },
  description: { label: "Description", text: (one) => one.description },
  changelog: { label: "Change note", text: (one) => one.changelog },
  tags: { label: "Tags", text: (one) => (one.tags ?? []).join("\n") },
  dlc: { label: "Required DLC", text: (one) => (one.dlc ?? []).join("\n") },
  previews: {
    label: "Screenshots and videos",
    text: (one) => (one.previews ?? []).map((p) => p.path || p.url).join("\n"),
  },
  visibility: { label: "Visibility", text: (one) => one.visibility },
};
const STATUS = { added: "A", changed: "M", removed: "D" } as const;
const STATUS_COLORS = { A: "text-success", M: "text-warning", D: "text-error" };
const PANES = [
  { slot: "list", sizeUnit: "px" as const, defaultSize: 330, minSize: 240, maxSize: 560 },
  { slot: "compare" },
];

const picked = shallowRef("");
const rows = computed(() => [
  ...(comparison.listing ?? []).map((field) => ({
    id: `field:${field}`,
    label: FIELDS[field]?.label ?? field,
    mark: "M" as const,
    field,
  })),
  ...(["added", "changed", "removed"] as const).flatMap((status) =>
    (comparison[status] ?? []).map((rel) => ({ id: `file:${rel}`, label: rel, mark: STATUS[status], rel })),
  ),
]);
const current = computed(() => rows.value.find((row) => row.id === picked.value) ?? rows.value[0]);
const kept = computed(() => new Date(comparison.uploadedAt).getFullYear() > 1);
const { data: file } = useQuery(() => ({
  ...comparedFileQuery({ modId, rel: current.value && "rel" in current.value ? current.value.rel : "" }),
  enabled: !!current.value && "rel" in current.value,
}));
const sides = computed(() => {
  const row = current.value;
  if (row && "field" in row) {
    const text = FIELDS[row.field]?.text;
    return text ? [text(comparison.last), text(listing)] : ["", ""];
  }
  return file.value ?? ["", ""];
});
const steamSide = computed(() =>
  comparison.downloaded
    ? `Steam side: Steam's copy, as it was last updated on ${day(comparison.uploadedAt)}.`
    : `Steam side: the copy PMT kept when it uploaded v${comparison.last.version} on ${day(comparison.uploadedAt)}.`,
);
</script>

<template>
  <div class="flex h-full min-h-0 flex-col gap-2.5">
    <div class="flex shrink-0 items-center gap-1.5 text-12 whitespace-nowrap text-dimmed">
      <template v-if="kept">
        <UBadge :label="`Steam v${comparison.last.version}`" color="info" size="sm" />
        <UIcon name="i-lucide-chevron-right" class="size-3.5" />
        <UBadge :label="`Workspace v${listing.version}`" color="success" size="sm" />
        <span class="ms-2">{{ plural(rows.length, "difference") }}</span>
      </template>
      <span class="grow" />
      <span v-for="sentence in steamSentences(comparison.steam ?? null)" :key="sentence">{{ sentence }}</span>
    </div>
    <p v-if="!kept" class="text-13 text-dimmed">
      PMT keeps a copy of what it uploads, and has none for this mod. Download Steam's copy to compare with it; Steam
      must be running.
    </p>
    <p v-else-if="!rows.length" class="text-13 text-dimmed">Nothing differs: the next upload sends what Steam has.</p>
    <USplitter
      v-else
      :items="PANES"
      auto-save-id="pmt.publish.compare"
      class="min-h-0 grow overflow-hidden rounded-lg border border-default"
    >
      <template #list>
        <section class="flex min-h-0 min-w-0 flex-1 flex-col border-e border-default bg-muted">
          <div class="min-h-0 grow overflow-y-auto py-1.5">
            <button
              v-for="row in rows"
              :key="row.id"
              type="button"
              class="flex h-7 w-full items-center gap-2 ps-3 pe-2.5 text-start text-12 text-toned hover:bg-elevated"
              :class="{ 'bg-(--accent-soft) text-highlighted': row.id === current?.id }"
              @click="picked = row.id"
            >
              <b class="w-3.5 shrink-0 text-center font-mono font-medium" :class="STATUS_COLORS[row.mark]">{{
                row.mark
              }}</b>
              <span class="truncate" :class="{ 'font-mono': 'rel' in row }">{{ row.label }}</span>
            </button>
          </div>
          <p class="shrink-0 border-t border-default px-3 py-2.5 text-12 text-dimmed">{{ steamSide }}</p>
        </section>
      </template>
      <template #compare>
        <CompareView
          :left="sides[0] ?? ''"
          :right="sides[1] ?? ''"
          language="plaintext"
          :left-label="`On Steam, v${comparison.last.version}`"
          :right-label="`Saved in the workspace, v${listing.version}`"
        />
      </template>
    </USplitter>
  </div>
</template>
