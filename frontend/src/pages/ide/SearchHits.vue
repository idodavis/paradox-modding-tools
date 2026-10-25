<!-- The hits of the running search, grouped by file. A hit opens its place in the editor, at the match. -->
<script setup lang="ts">
import { computed, shallowRef } from "vue";

import type { Hit } from "@services/search/models";

import { plural } from "../../lib/format";
import { useEditorStore } from "../../stores/editor";
import { useSearchStore } from "../../stores/search";
import { useWorkspaceStore } from "../../stores/workspace";
import OriginChip from "../../ui/OriginChip.vue";

/** How many hits are listed open before the next files are folded. */
const OPEN_HITS = 500;

const editor = useEditorStore();
const search = useSearchStore();
const workspace = useWorkspaceStore();
// the files clicked, which are folded if they would be open and open if they would be folded
const flips = shallowRef(new Set<string>());

// a folded file's lines are not split around their match until it opens
const files = computed(() => {
  let lineCount = 0;
  return search.current().files.map(({ place, hits }) => {
    const count = hits?.length ?? 0;
    const open = lineCount < OPEN_HITS !== flips.value.has(place.key);
    if (open) lineCount += count;
    const folder = [workspace.originName(place.origin), place.folder].filter(Boolean).join(" › ");
    return { place, count, lines: open ? (hits ?? []).map(describe) : [], open, folder };
  });
});
const count = computed(() => files.value.reduce((sum, file) => sum + file.count, 0));

/** A hit's line split around its match, with the text before the match trimmed at its start. */
function describe(hit: Hit): { line: number; column: number; before: string; match: string; after: string } {
  const { text, range } = hit;
  return {
    line: range.startLineNumber,
    column: range.startColumn,
    before: text.slice(0, range.startColumn - 1).trimStart(),
    match: text.slice(range.startColumn - 1, range.endColumn - 1),
    after: text.slice(range.endColumn - 1),
  };
}

/** Folds an open file's hits, or opens a folded file's. */
function flip(key: string): void {
  const next = new Set(flips.value);
  if (!next.delete(key)) next.add(key);
  flips.value = next;
}
</script>

<template>
  <section v-if="search.current().query || search.current().title" class="py-1">
    <p class="flex flex-wrap items-center gap-x-2 px-2.5 pb-1 text-11 text-dimmed">
      <span v-if="search.current().title" class="font-medium text-toned">{{ search.current().title }}</span>
      {{ plural(count, "result") }} in {{ plural(files.length, "file") }}
      <span v-if="search.current().running">· searching…</span>
      <UBadge
        v-if="search.current().capped"
        label="Stopped here: narrow the search to see the rest"
        color="warning"
        variant="subtle"
        size="sm"
      />
    </p>
    <template v-for="file in files" :key="file.place.key">
      <button
        type="button"
        class="flex h-6 w-full items-center gap-1.5 ps-1.5 pe-2.5 text-start text-12 whitespace-nowrap hover:bg-accented"
        @click="flip(file.place.key)"
      >
        <UIcon
          :name="file.open ? 'i-lucide-chevron-down' : 'i-lucide-chevron-right'"
          class="size-3.5 shrink-0 text-dimmed"
        />
        <OriginChip :origin="file.place.origin" dot />
        <span class="shrink-0 text-highlighted">{{ file.place.name }}</span>
        <span class="min-w-0 truncate text-11 text-dimmed">{{ file.folder }}</span>
        <UBadge :label="String(file.count)" size="sm" class="ms-auto shrink-0" />
      </button>
      <template v-if="file.open">
        <button
          v-for="(hit, index) in file.lines"
          :key="index"
          type="button"
          class="flex h-5.5 w-full items-center ps-7 pe-2.5 text-start font-mono text-11 whitespace-nowrap text-toned hover:bg-accented"
          :title="`${file.place.rel}:${hit.line}`"
          @click="editor.show(file.place, { lineNumber: hit.line, column: hit.column }, 'navigate')"
        >
          <span class="truncate"
            >{{ hit.before }}<b class="rounded-xs bg-(--accent-soft) font-medium text-highlighted">{{ hit.match }}</b
            >{{ hit.after }}</span
          >
        </button>
      </template>
    </template>
    <p v-if="!files.length && !search.current().running" class="px-2.5 py-1 text-12 text-dimmed">No results.</p>
  </section>
</template>
