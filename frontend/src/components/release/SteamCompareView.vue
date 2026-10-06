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
import type { TabsItem } from "@nuxt/ui";
import type { Listing, ListingChange } from "@services/models";
import { DownloadFromSteam } from "@services/releaseservice";
import { diffView, monaco } from "../../lib/code";
import { compareQuery, releaseKeys, textQuery, uploadFilesQuery } from "../../queries";
import { day, list, plural } from "../../lib/format";
import { useSettingsStore } from "../../stores/settings";

const { listing } = defineProps<{ listing: Listing }>();
const emit = defineEmits<{ close: [] }>();

const settings = useSettingsStore();
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
// a side a file lacks is null, so a new or removed file is all added or all removed
const sides = computed((): [string | null, string | null] | undefined => {
  const row = current.value;
  // a field's value ends its line, so the compare says nothing of a missing last newline
  if (row?.texts) return [row.texts[0].replace(/\n?$/, "\n"), row.texts[1].replace(/\n?$/, "\n")];
  if (!row?.files) return undefined;
  const side = (path: string, text: string | undefined) => (!path ? null : text);
  const [old, now] = [side(row.files[0], before.value?.text), side(row.files[1], after.value?.text)];
  return old === undefined || now === undefined ? undefined : [old, now];
});
const kind = computed(() => (!sides.value ? undefined : sides.value[0] === null ? "new" : sides.value[1] === null ? "deleted" : "change"));
const unchanged = computed(() => !!sides.value && sides.value[0] === sides.value[1]);
// the header has a column per side only where both are drawn
const split = computed(() => style.value === "split" && kind.value === "change");

// one view, its texts swapped as the row changes; its unchanged lines fold into bars unless every line is asked for
const all = useLocalStorage("pmt.publish.compareAll", false);
const host = useTemplateRef("host");
const view = shallowRef<ReturnType<typeof diffView>>();
/** A line of a side, 1-based: the old file's (deletions) or the new's (additions), and the new file's line beside it. */
type DiffLine = { side: "deletions" | "additions"; line: number; at: number };
const lineChanges = shallowRef<monaco.editor.ILineChange[]>([]);
watch([host, sides], ([el, next], [was]) => {
  // an unchanged row takes the view's element away; the next change draws a new one
  if (el !== was) {
    view.value?.dispose();
    view.value = undefined;
  }
  if (!el || !next || next[0] === next[1]) return;
  if (!view.value) {
    view.value = diffView(el, settings.codeFont, style.value === "split", all.value);
    view.value.view.onDidUpdateDiff(() => (lineChanges.value = view.value?.view.getLineChanges() ?? []));
  }
  const name = current.value?.texts ? "field.text" : (current.value?.label ?? "");
  view.value.show({ text: next[0] ?? "", name }, { text: next[1] ?? "", name });
  at.value = -1;
});
watch([style, all], () => view.value?.view.updateOptions({ renderSideBySide: style.value === "split", hideUnchangedRegions: { enabled: !all.value } }));
onBeforeUnmount(() => view.value?.dispose());

/** Show a line: the new file's where it is drawn, else the old file's beside the new one's place. */
function show(line: DiffLine | undefined): void {
  const shown = view.value?.view;
  if (!shown || !line) return;
  const editor = line.side === "additions" || !split.value ? shown.getModifiedEditor() : shown.getOriginalEditor();
  const number = line.side === "additions" || split.value ? line.line : line.at;
  editor.revealLineInCenter(number);
  editor.setSelection(new monaco.Range(number, 1, number, 1));
}

// each change's first line, on the side it adds to, else the side it removes from
const changes = computed((): DiffLine[] =>
  lineChanges.value.map((change) =>
    change.modifiedEndLineNumber > 0
      ? { side: "additions", line: change.modifiedStartLineNumber, at: change.modifiedStartLineNumber }
      : { side: "deletions", line: change.originalStartLineNumber, at: Math.max(change.modifiedStartLineNumber, 1) },
  ),
);
const at = shallowRef(-1);
function stepChange(by: number): void {
  const count = changes.value.length;
  at.value = at.value < 0 ? (by > 0 ? 0 : count - 1) : (at.value + by + count) % count;
  show(changes.value[at.value]);
}

// Find: every line of either side holding the text, in the order the compare shows them; a line both sides share
// is found once, on the workspace's side, and a removed line sits before the lines its change adds
const query = shallowRef("");
const hits = computed((): DiffLine[] => {
  const needle = query.value.trim().toLowerCase();
  if (!needle || !sides.value) return [];
  const removed = new Map<number, { place: number; at: number }>();
  for (const change of lineChanges.value) {
    const count = change.originalEndLineNumber - change.originalStartLineNumber + 1;
    if (change.originalEndLineNumber === 0) continue;
    const next = change.modifiedEndLineNumber > 0 ? change.modifiedStartLineNumber - 1 : change.modifiedStartLineNumber;
    for (let i = 0; i < count; i++) removed.set(change.originalStartLineNumber + i, { place: next - 1 + (i + 1) / (count + 1), at: Math.max(next, 1) });
  }
  const found = (text: string | null, side: DiffLine["side"], order: (n: number) => { place: number; at: number } | undefined) =>
    (text ?? "").split("\n").flatMap((line, i) => {
      const spot = order(i + 1);
      return spot && line.toLowerCase().includes(needle) ? [{ side, line: i + 1, ...spot }] : [];
    });
  return [...found(sides.value[0], "deletions", (n) => removed.get(n)), ...found(sides.value[1], "additions", (n) => ({ place: n - 1, at: n }))]
    .sort((a, b) => a.place - b.place)
    .map(({ side, line, at }) => ({ side, line, at }));
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
              <div v-if="kind !== 'new'" class="flex h-full items-center gap-2 px-3" :class="{ 'flex-1 border-e border-default': split }">
                <UIcon name="i-lucide-cloud" class="size-3.5" />On Steam, v{{ steam.version }}<span class="text-11 text-dimmed">{{ updated }}</span>
              </div>
              <UIcon v-if="!split && kind === 'change'" name="i-lucide-arrow-right" class="size-3.5 text-dimmed" />
              <div v-if="kind !== 'deleted'" class="flex h-full items-center gap-2 px-3" :class="{ 'flex-1': split }">
                <UIcon name="i-lucide-save" class="size-3.5" />Saved in the workspace, v{{ listing.version }}
                <span class="text-11 text-dimmed">{{ savedAgo }}</span>
              </div>
            </div>
            <p v-if="current && !current.texts && !current.files" class="p-4 text-12 text-dimmed">A binary file: no lines to compare.</p>
            <p v-else-if="unchanged" class="p-4 text-12 text-dimmed">Unchanged: Steam has the same {{ current?.label }}.</p>
            <div v-else ref="host" class="min-h-0 grow" />
          </div>
        </div>
      </div>
    </template>
  </UModal>
</template>
