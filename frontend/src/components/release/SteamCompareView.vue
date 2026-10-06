<script setup lang="ts">
/**
 * Compare with Steam (the PublishCompare board), opened full screen over Publish from its upload checklist and
 * its state bar (O-R40): the listing's fields, then the files an upload changes, each with the lines it adds
 * and removes, and the one picked drawn as Steam has it beside the workspace's, Split or Unified. Its unchanged
 * lines fold into sections that open; Previous and Next step through its changes, and Find (Ctrl F) through
 * the lines of both sides. Steam's side is the snapshot PMT kept at its last upload, or a download from Steam.
 * Opened through useOverlay.
 */
import { computed, onBeforeUnmount, shallowRef, useTemplateRef, watch } from "vue";
import { useLocalStorage, useTimeAgo } from "@vueuse/core";
import { useMutation, useQuery, useQueryCache } from "@pinia/colada";
import { parseDiffFromFile, type FileContents, type FileDiff } from "@pierre/diffs";
import type { TabsItem } from "@nuxt/ui";
import type { Listing, ListingChange } from "@services/models";
import { DownloadFromSteam } from "@services/releaseservice";
import { codeFile, diffView, showLine, type DiffLine } from "../../lib/code";
import { compareQuery, releaseKeys, textQuery, uploadFilesQuery } from "../../queries";
import { day, list, plural } from "../../lib/format";

const { listing } = defineProps<{ listing: Listing }>();
const emit = defineEmits<{ close: [] }>();

const ids = computed(() => ({ workspaceId: listing.workspaceId, modId: listing.modId }));
const { data: steam } = useQuery(() => compareQuery(ids.value));
const style = shallowRef<"split" | "unified">("split");
const styles: TabsItem[] = [
  { label: "Split", value: "split" },
  { label: "Unified", value: "unified" },
];

const STATUS: Record<string, string> = { "=": "text-dimmed", M: "text-warning", A: "text-success", D: "text-error" };
const ROW_UI = {
  root: "gap-0 rounded-none ring-0",
  content: "max-h-none",
  group: "p-0",
  item: [
    "h-7 items-center gap-2 rounded-none ps-3 pe-2.5 text-12 text-toned before:rounded-none",
    "data-[state=checked]:border-s-2 data-[state=checked]:border-primary data-[state=checked]:text-highlighted data-[state=checked]:before:bg-(--accent-soft)",
  ],
  itemTrailingIcon: "hidden",
};

/** One row: a listing field (its two values) or a file (its two paths, "" for a side without it). */
type Row = { value: string; status: string; label: string; detail?: string; texts?: [string, string]; files?: [string, string] };

/** What a field's change says beside it: the tags added and removed, else its old and new value. */
function detail(row: ListingChange): string | undefined {
  if (row.field === "Description") return undefined;
  if (!row.added?.length && !row.removed?.length) return `${row.steam || "none"} to ${row.workspace || "none"}`;
  return [row.added?.length ? `${list(row.added)} added` : "", row.removed?.length ? `${list(row.removed)} removed` : ""]
    .filter(Boolean)
    .join(", ");
}

const fields = computed(() =>
  (steam.value?.listing ?? []).map(
    (row): Row => ({
      value: `field:${row.field}`,
      status: row.changed ? "M" : "=",
      label: row.field,
      detail: row.changed ? detail(row) : "",
      texts: [row.steam, row.workspace],
    }),
  ),
);
const files = computed(() =>
  (steam.value?.files ?? []).map(
    (file): Row => ({
      value: `file:${file.rel}`,
      status: file.status,
      label: file.rel,
      detail: file.binary ? "binary" : undefined,
      files: file.binary ? undefined : [file.steam, file.workspace],
    }),
  ),
);
// the fields an upload changes: the rest of its changes are files
const changedFields = computed(() => (steam.value?.changes ?? 0) - files.value.length);
const { data: upload } = useQuery(() => uploadFilesQuery(ids.value));

const picked = shallowRef("");
const current = computed(() => {
  const rows = [...fields.value, ...files.value];
  return rows.find((row) => row.value === picked.value) ?? rows.find((row) => row.status !== "=") ?? rows[0];
});

// the picked row's two sides, a file's read as the editor reads it
const { data: before } = useQuery(() => textQuery(current.value?.files?.[0] ?? ""));
const { data: after } = useQuery(() => textQuery(current.value?.files?.[1] ?? ""));
// a side a file lacks is null, so Pierre draws a new or removed file as one column
const sides = computed((): [FileContents | null, FileContents | null] | undefined => {
  const row = current.value;
  // a field's value ends its line, so the diff says nothing of a missing last newline
  const prose = (contents: string): FileContents => ({ name: row?.label ?? "", contents: contents.replace(/\n?$/, "\n"), lang: "text" });
  if (row?.texts) return [prose(row.texts[0]), prose(row.texts[1])];
  if (!row?.files) return undefined;
  const side = (path: string, text: string | undefined) => (!path ? null : text === undefined ? undefined : codeFile(row.label, text));
  const [old, now] = [side(row.files[0], before.value), side(row.files[1], after.value)];
  return old === undefined || now === undefined ? undefined : [old, now];
});
const diff = computed(() => sides.value && parseDiffFromFile(...sides.value));
// the header has a column per side only where Pierre draws two
const split = computed(() => style.value === "split" && diff.value?.type === "change");

// a view per diff, since Pierre keeps the sections it opened until its view is cleaned up; the style and
// "Show all lines" change in place
const all = useLocalStorage("pmt.publish.compareAll", false);
const host = useTemplateRef("host");
let view: FileDiff | undefined;
watch([host, diff], ([el, next]) => {
  view?.cleanUp();
  view = el && next?.hunks.length ? diffView(style.value, !all.value) : undefined;
  view?.render({ fileDiff: next, containerWrapper: el ?? undefined });
  at.value = -1;
});
watch([style, all], () => {
  view?.setOptions({ ...view.options, diffStyle: style.value, expandUnchanged: all.value });
  view?.rerender();
});
onBeforeUnmount(() => view?.cleanUp());
const show = (line: DiffLine | undefined) => view && host.value && line && void showLine(view, host.value, line);

// each change's first line, on the side it adds to, else the side it removes from
const changes = computed((): DiffLine[] =>
  (diff.value?.hunks ?? []).flatMap((hunk) =>
    hunk.hunkContent.flatMap((part): DiffLine[] =>
      part.type !== "change" ? [] : [part.additions ? { side: "additions", line: part.additionLineIndex + 1 } : { side: "deletions", line: part.deletionLineIndex + 1 }],
    ),
  ),
);
const at = shallowRef(-1);
function stepChange(by: number): void {
  const count = changes.value.length;
  at.value = at.value < 0 ? (by > 0 ? 0 : count - 1) : (at.value + by + count) % count;
  show(changes.value[at.value]);
}

// Find: every line of either side holding the text, in the order the diff shows them; a line both sides
// share is found once, on the workspace's side, and a removed line sits before the lines its change adds
const query = shallowRef("");
const hits = computed((): DiffLine[] => {
  const needle = query.value.trim().toLowerCase();
  if (!needle || !diff.value) return [];
  const removed = new Map<number, number>();
  for (const part of diff.value.hunks.flatMap((hunk) => hunk.hunkContent)) {
    if (part.type !== "change") continue;
    for (let i = 0; i < part.deletions; i++) removed.set(part.deletionLineIndex + i, part.additionLineIndex - 1 + (i + 1) / (part.deletions + 1));
  }
  const found = (lines: string[], side: DiffLine["side"], order: (i: number) => number | undefined) =>
    lines.flatMap((text, i) => {
      const place = order(i);
      return place !== undefined && text.toLowerCase().includes(needle) ? [{ side, line: i + 1, place }] : [];
    });
  return [...found(diff.value.deletionLines, "deletions", (i) => removed.get(i)), ...found(diff.value.additionLines, "additions", (i) => i)]
    .sort((a, b) => a.place - b.place)
    .map(({ side, line }) => ({ side, line }));
});
const hit = shallowRef(0);
watch(hits, (next) => {
  hit.value = 0;
  show(next[0]);
});
function stepHit(by: number): void {
  if (!hits.value.length) return;
  hit.value = (hit.value + by + hits.value.length) % hits.value.length;
  show(hits.value[hit.value]);
}

const width = useLocalStorage("pmt.publish.compareWidth", 330);
const updated = computed(() => (steam.value?.uploadedAt ? day(steam.value.uploadedAt) : ""));
const savedAgo = useTimeAgo(() => listing.saved || Date.now());

const queryCache = useQueryCache();
const { mutate: download, isLoading: downloading } = useMutation({
  mutation: () => DownloadFromSteam(listing.workspaceId, listing.modId),
  onSuccess: (next) => queryCache.setQueryData(releaseKeys.compare(listing.workspaceId, listing.modId), next),
});
</script>

<template>
  <UModal title="Compare with Steam" fullscreen @update:open="emit('close')">
    <template #body>
      <div class="flex h-full min-h-0 flex-col gap-2.5">
        <div v-if="steam?.from" class="flex shrink-0 items-center gap-1.5 whitespace-nowrap">
          <UBadge :label="`Steam v${steam.version}`" color="info" size="sm" />
          <UIcon name="i-lucide-chevron-right" class="size-3.5 text-dimmed" />
          <UBadge :label="`Workspace v${listing.version}`" color="success" size="sm" />
          <span class="ms-2 text-12 text-dimmed">
            {{ steam.changes }} differences: {{ changedFields }} in the listing, {{ files.length }} in files
          </span>
          <span class="grow" />
          <UTabs :model-value="style" :items="styles" :content="false" @update:model-value="style = $event === 'unified' ? 'unified' : 'split'" />
        </div>
        <p v-if="!steam" class="text-13 text-dimmed">Reading what Steam has…</p>
        <p v-else-if="!steam.item" class="text-13 text-dimmed">This mod is not on Steam yet, so there is nothing to compare it with.</p>
        <div v-else-if="!steam.from" class="flex flex-col items-start gap-3">
          <p class="text-13 text-dimmed">
            PMT keeps a copy of what it uploads, and has none for this mod. Download Steam's copy to compare with it; Steam must be
            running.
          </p>
          <UButton label="Download from Steam" icon="i-lucide-download" :loading="downloading" @click="download()" />
        </div>
        <div v-else class="flex min-h-0 grow overflow-hidden rounded-lg border border-default">
          <section class="relative flex min-h-0 shrink-0 flex-col border-e border-default bg-muted py-1.5" :style="{ width: `${width}px` }">
            <div class="flex h-7.5 items-center gap-2 px-3 text-11 font-semibold text-toned">
              Listing<span class="font-normal text-dimmed">{{ changedFields }} of {{ fields.length }} changed</span>
            </div>
            <UListbox :model-value="current?.value" :items="fields" value-key="value" :ui="ROW_UI" @update:model-value="picked = String($event)">
              <template #item="{ item }">
                <b class="w-3.5 shrink-0 text-center font-mono font-medium" :class="STATUS[item.status]">{{ item.status }}</b>
                <span class="truncate">{{ item.label }}</span>
                <ChangeCounts v-if="item.status === 'M' && !item.detail" :texts="item.texts" />
                <span v-else-if="item.detail" class="ms-auto truncate font-mono text-11 text-dimmed">{{ item.detail }}</span>
              </template>
            </UListbox>
            <div class="mt-2 flex h-9 items-center gap-2 border-t border-default px-3 pt-1 text-11 font-semibold text-toned">
              Files<span class="font-normal text-dimmed">{{ files.length }} changed{{ upload ? ` of ${upload.count}` : "" }}</span>
            </div>
            <UListbox
              :model-value="current?.value"
              :items="files"
              value-key="value"
              :ui="{ ...ROW_UI, root: 'min-h-0 grow gap-0 overflow-auto rounded-none ring-0' }"
              @update:model-value="picked = String($event)"
            >
              <template #item="{ item }">
                <b class="w-3.5 shrink-0 text-center font-mono font-medium" :class="STATUS[item.status]">{{ item.status }}</b>
                <span class="truncate">{{ item.label }}</span>
                <ChangeCounts v-if="item.files" :files="item.files" />
                <span v-else class="ms-auto font-mono text-11 text-dimmed">{{ item.detail }}</span>
              </template>
            </UListbox>
            <div class="mx-2.5 mt-2 mb-1 flex flex-col gap-1.5 rounded-md border border-default bg-default p-2.5">
              <span class="text-12 text-toned">
                Steam side:
                {{ steam.from === "download" ? `downloaded from Steam, as it was last updated on ${updated}.` : `the snapshot PMT saved when it uploaded v${steam.version} on ${updated}.` }}
              </span>
              <UButton label="Download from Steam instead" icon="i-lucide-download" :loading="downloading" class="self-start" @click="download()" />
            </div>
          </section>
          <ResizeGrip v-model="width" :min="240" :max="560" :default-size="330" />
          <div class="flex min-w-0 grow flex-col">
            <div class="flex h-10 shrink-0 items-center gap-1 border-b border-default px-2.5">
              <UButton icon="i-lucide-chevron-up" variant="ghost" size="xs" square :disabled="!changes.length" aria-label="Previous change" @click="stepChange(-1)" />
              <UButton icon="i-lucide-chevron-down" variant="ghost" size="xs" square :disabled="!changes.length" aria-label="Next change" @click="stepChange(1)" />
              <span class="ms-1 text-12 text-dimmed tabular-nums">
                {{ at < 0 ? plural(changes.length, "change") : `Change ${at + 1} of ${changes.length}` }}
              </span>
              <USwitch v-model="all" label="Show all lines" size="sm" class="ms-4" :ui="{ label: 'text-12 font-normal text-toned' }" />
              <span class="grow" />
              <FindBox v-model="query" :hit="hit" :hits="hits.length" placeholder="Find in both sides" class="w-60" @step="stepHit" />
            </div>
            <div class="flex h-8.5 shrink-0 items-center border-b border-default bg-muted text-12 whitespace-nowrap text-toned">
              <div v-if="diff?.type !== 'new'" class="flex h-full items-center gap-2 px-3" :class="{ 'flex-1 border-e border-default': split }">
                <UIcon name="i-lucide-cloud" class="size-3.5" />On Steam, v{{ steam.version }}<span class="text-11 text-dimmed">{{ updated }}</span>
              </div>
              <UIcon v-if="!split && diff?.type === 'change'" name="i-lucide-arrow-right" class="size-3.5 text-dimmed" />
              <div v-if="diff?.type !== 'deleted'" class="flex h-full items-center gap-2 px-3" :class="{ 'flex-1': split }">
                <UIcon name="i-lucide-save" class="size-3.5" />Saved in the workspace, v{{ listing.version }}
                <span class="text-11 text-dimmed">{{ savedAgo }}</span>
              </div>
            </div>
            <p v-if="current && !current.texts && !current.files" class="p-4 text-12 text-dimmed">A binary file: no lines to compare.</p>
            <p v-else-if="diff && !diff.hunks.length" class="p-4 text-12 text-dimmed">Unchanged: Steam has the same {{ current?.label }}.</p>
            <div v-else ref="host" class="min-h-0 grow overflow-auto bg-(--bg-0)" />
          </div>
        </div>
      </div>
    </template>
  </UModal>
</template>
