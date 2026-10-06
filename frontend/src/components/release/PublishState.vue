<script setup lang="ts">
/**
 * Publish's state bar: the edits here and not saved, what the workspace has saved, and what Steam has, each
 * a cell with its mark; Steam's says when the item was last updated on the listing step and what an upload
 * changes on Review. On the listing step Steam's opens Compare, which Review's checks open there.
 */
import { computed } from "vue";
import { useTimeAgo } from "@vueuse/core";
import type { Listing, SteamCompare } from "@services/models";
import { capital, day, list, plural } from "../../lib/format";

const { unsaved, listing, steam, review = false } = defineProps<{
  unsaved: string[];
  listing: Listing;
  steam?: SteamCompare | null;
  review?: boolean;
}>();
const emit = defineEmits<{ compare: [] }>();

const TONES = {
  orange: "bg-(--orange-soft) text-warning",
  green: "bg-(--green-soft) text-success",
  blue: "bg-(--blue-soft) text-info",
};

const savedAgo = useTimeAgo(() => listing.saved || Date.now());
// what an upload changes on Steam: "14 files and the description"
const changes = computed(() => {
  const files = steam?.files?.length ?? 0;
  const fields = (steam?.listing ?? []).filter((row) => row.changed).map((row) => `the ${row.field.toLowerCase()}`);
  return list([...(files ? [plural(files, "file")] : []), ...fields]);
});

const cells = computed(() => {
  const here = unsaved.length
    ? { icon: "i-lucide-pencil", tone: TONES.orange, value: plural(unsaved.length, "unsaved change"), sub: capital(list(unsaved)) }
    : { icon: "i-lucide-check", tone: TONES.green, value: "Nothing unsaved", sub: "Every edit is in the workspace" };
  const saved = {
    icon: "i-lucide-save",
    tone: TONES.green,
    value: `v${listing.version}`,
    sub: review ? "This is what will be uploaded" : `${listing.descriptor}, ${savedAgo.value}`,
  };
  const updated = steam?.uploadedAt ? day(steam.uploadedAt) : "";
  let onSteam = { icon: "i-lucide-cloud", tone: TONES.blue, value: "Not published", sub: "Uploading creates the Workshop item" };
  if (steam?.item && !steam.from) onSteam = { ...onSteam, value: steam.item, sub: "No copy of it to compare yet" };
  else if (steam?.from) {
    const same = !changes.value;
    onSteam = {
      icon: "i-lucide-cloud",
      tone: review && !same ? TONES.orange : TONES.blue,
      value: `v${steam.version}`,
      sub: review
        ? same ? "Nothing will change" : `${capital(changes.value)} will change`
        : `Last updated ${updated}`,
    };
  }
  return [
    { label: "Here, not saved", mono: false, ...here },
    { label: "Saved in the workspace", mono: true, ...saved },
    { label: "On Steam", mono: !!steam?.item, ...onSteam },
  ];
});
</script>

<template>
  <div class="flex shrink-0 items-stretch overflow-hidden rounded-lg border border-default bg-muted">
    <template v-for="(cell, i) in cells" :key="cell.label">
      <UIcon v-if="i" name="i-lucide-chevron-right" class="size-3.5 shrink-0 self-center text-dimmed" />
      <div class="flex min-w-0 flex-1 basis-0 items-center gap-3 px-3.5 py-2.5">
        <span class="flex size-7.5 shrink-0 items-center justify-center rounded-lg" :class="cell.tone">
          <UIcon :name="cell.icon" class="size-3.5" />
        </span>
        <span class="flex min-w-0 grow flex-col">
          <span class="text-11 whitespace-nowrap text-dimmed">{{ cell.label }}</span>
          <span class="text-13 font-semibold whitespace-nowrap text-highlighted" :class="{ 'font-mono': cell.mono }">{{ cell.value }}</span>
          <span class="truncate text-11 text-dimmed" :title="cell.sub">{{ cell.sub }}</span>
        </span>
        <UButton v-if="i === 2 && steam?.item && !review" label="Compare" @click="emit('compare')" />
      </div>
    </template>
  </div>
</template>
