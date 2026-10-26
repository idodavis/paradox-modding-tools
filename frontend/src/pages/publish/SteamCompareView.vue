<!--
  This lists every difference the next upload makes on Steam, and shows the picked one beside Steam's copy.
-->
<script setup lang="ts">
import { computed } from "vue";
import { useQuery } from "@pinia/colada";
import { onKeyStroke } from "@vueuse/core";

import { useShown } from "../../app/pages";
import { day, describeAge, describeDifference, toSentence } from "../../lib/format";
import { comparedFileQuery, fileUrl, uploadQuery, useComparison } from "../../queries";
import { useWorkspaceStore } from "../../stores/workspace";
import CompareView from "../../ui/CompareView.vue";
import StatusFacts from "../../ui/StatusFacts.vue";
import { Format } from "@internal/files/models";
import type { Comparison, Listing } from "@services/steam/models";

/** FACTS is the status bar's hint on how to leave the compare. */
const FACTS = [{ label: "Esc", value: "returns to the page" }];

const { modId, listingKey, listing, comparison, picked } = defineProps<{
  modId: string;
  listingKey: string;
  listing: Listing;
  comparison: Comparison;
  picked: string;
}>();
const emit = defineEmits<{ "update:picked": [id: string]; back: [] }>();

const workspace = useWorkspaceStore();
const enabled = computed(() => workspace.isOpen());
const { data: upload } = useQuery(() => ({ ...uploadQuery({ modId, key: listingKey }), enabled: enabled.value }));
const { mutate: check, isLoading: checking } = useComparison();
const visible = useShown();

const groups = computed(() =>
  [
    { label: "Page", differences: comparison.page ?? [] },
    { label: "Files", differences: comparison.files ?? [] },
  ].map(({ label, differences }) => ({
    label,
    rows: differences.map((difference) => ({ difference, ...describeDifference(difference) })),
  })),
);
const current = computed(() => groups.value.flatMap((group) => group.rows).find((row) => row.id === picked));
const file = computed(() =>
  current.value?.difference.field === "file"
    ? upload.value?.files?.find((one) => one.rel === current.value?.difference.rel)
    : undefined,
);
const textual = computed(() => file.value?.format !== Format.FormatImage && file.value?.format !== Format.FormatBinary);
const { data: sides } = useQuery(() => ({
  ...comparedFileQuery({ modId, key: listingKey, rel: current.value?.difference.rel ?? "" }),
  enabled: enabled.value && current.value?.difference.field === "file" && textual.value,
}));
// Steam's text first, then the mod's.
const texts = computed(() => {
  const difference = current.value?.difference;
  if (!difference) return ["", ""];
  if (difference.field === "file") return [sides.value?.[0] ?? "", sides.value?.[1] ?? ""];
  return [difference.before ?? "", difference.after ?? ""];
});

// Escape goes back, unless a dialog is open over the page or something has used the key already.
onKeyStroke("Escape", (event) => {
  if (visible.value && !event.defaultPrevented && !document.querySelector("[role=dialog]")) emit("back");
});
</script>

<template>
  <div class="flex h-full min-h-0 flex-col">
    <div class="flex h-14 shrink-0 items-center gap-2.5 border-b border-default ps-3.5 pe-4">
      <UTooltip text="Back to the page (Esc)">
        <UButton
          icon="i-lucide-chevron-left"
          variant="ghost"
          color="neutral"
          square
          aria-label="Back to the page"
          @click="emit('back')"
        />
      </UTooltip>
      <span class="shrink-0 text-13 font-semibold text-highlighted">What changes on Steam</span>
      <span class="truncate text-12 text-dimmed">Steam asked {{ describeAge(comparison.checkedAt, "short") }}</span>
      <span class="grow" />
      <UButton
        label="Check again"
        icon="i-lucide-refresh-cw"
        color="neutral"
        variant="subtle"
        :loading="checking"
        @click="check({ modId, key: listingKey })"
      />
    </div>

    <p v-if="comparison.status === 'unknown'" class="p-5 text-13 text-dimmed">{{ toSentence(comparison.reason) }}</p>
    <p v-else-if="!current" class="p-5 text-13 text-dimmed">Nothing differs: the next upload sends what Steam has.</p>
    <div v-else class="flex min-h-0 grow">
      <nav class="flex w-65 shrink-0 flex-col overflow-y-auto border-e border-default bg-muted py-2">
        <template v-for="group in groups" :key="group.label">
          <div v-if="group.rows.length" class="px-3 pt-2 pb-0.5 text-11 font-semibold text-dimmed">
            {{ group.label }}
          </div>
          <button
            v-for="row in group.rows"
            :key="row.id"
            type="button"
            class="flex h-7 w-full items-center gap-2 ps-3 pe-2.5 text-start text-12 hover:bg-elevated"
            :class="{ 'bg-(--accent-soft) shadow-[inset_2px_0_0_var(--accent)]': row.id === current.id }"
            @click="emit('update:picked', row.id)"
          >
            <b class="w-3.5 shrink-0 text-center font-mono font-medium" :class="row.color">{{ row.mark }}</b>
            <span
              class="grow truncate text-highlighted"
              :class="{ 'font-mono text-11.5': row.difference.field === 'file' }"
              :title="row.label"
            >
              {{ row.label }}
            </span>
            <span class="max-w-24 shrink-0 truncate text-11 text-dimmed" :title="row.summary">{{ row.summary }}</span>
          </button>
        </template>
        <p class="px-3 pt-3 text-11 text-dimmed">Files the upload never sends are left out of this list.</p>
      </nav>

      <CompareView
        v-if="textual"
        :left="texts[0] ?? ''"
        :right="texts[1] ?? ''"
        language="plaintext"
        :left-label="comparison.steam ? `On Steam · ${day(comparison.steam.updatedAt)}` : 'On Steam'"
        :right-label="`Here · ${listing.version}`"
      />
      <div
        v-else-if="file?.format === Format.FormatImage"
        class="grid min-w-0 grow grid-cols-2 divide-x divide-default"
      >
        <div class="flex items-center justify-center p-4 text-12 text-dimmed">Steam's copy is not shown</div>
        <div class="flex items-center justify-center p-4">
          <img :src="fileUrl(file.path)" :alt="current.label" class="max-h-full max-w-full object-contain" />
        </div>
      </div>
      <p v-else class="grow p-5 text-13 text-dimmed">{{ current.label }} is not text, and {{ current.sentence }}.</p>
    </div>

    <StatusFacts :facts="FACTS" />
  </div>
</template>
