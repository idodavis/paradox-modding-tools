<script setup lang="ts">
/**
 * The IDE's panel (the Ide board's Problems and Search results, the IdeDiff board's Git output): the open files'
 * problems by file, filtered as typed, with Open in Health; a search's hits, or a word's references, by origin and
 * file, each line with its match in bold; and every git command source control ran, with what it wrote.
 */
import { computed, shallowRef, useTemplateRef, watch } from "vue";
import { useQuery } from "@pinia/colada";
import type { Result } from "../../stores/ide";
import { editorRange } from "../../ide/editorCore";
import { useIdeRoots } from "../../composables/useIdeRoots";
import { linesQuery } from "../../queries";
import { plural } from "../../lib/format";
import { useGitStore } from "../../stores/git";
import { useIdeStore } from "../../stores/ide";
import { useWorkspaceStore } from "../../stores/workspace";
import { Close } from "@services/windowservice";
import { isMain, openWindow, windowName } from "../../windows";

const { workspaceId } = defineProps<{ workspaceId: string }>();

const ide = useIdeStore();
const git = useGitStore();
const store = useWorkspaceStore();
const { locate } = useIdeRoots(() => workspaceId);

const filter = shallowRef("");
const counts = computed(() => {
  const all = [...ide.problems.values()].flat();
  return { errors: all.filter((found) => found.severity === 1).length, warnings: all.filter((found) => found.severity !== 1).length };
});
const files = computed(() => {
  const needle = filter.value.trim().toLowerCase();
  return [...ide.problems.entries()]
    .map(([path, found]) => {
      const at = locate(path);
      const rel = at?.rel ?? path;
      const shown = (found ?? []).filter(
        (one) => !needle || `${one.message} ${one.code} ${rel}`.toLowerCase().includes(needle),
      );
      return { path, rel, origin: at?.root.origin ?? "", found: shown };
    })
    .filter((file) => file.found.length);
});

const TABS = [
  { label: "Problems", value: "problems" },
  { label: "Search results", value: "search" },
  { label: "Git output", value: "git" },
];
const isTab = (value: string | number) => (value === "search" || value === "git" ? value : "problems");

// the Git output keeps its newest command in view
const gitLog = useTemplateRef("gitLog");
watch(
  () => [git.output.length, ide.panel],
  () => gitLog.value?.scrollTo({ top: gitLog.value.scrollHeight }),
  { flush: "post" },
);

// the results by origin, then file, in the order they came
const groups = computed(() => {
  const out: { origin: string; count: number; files: { rel: string; rows: Result[] }[] }[] = [];
  for (const row of ide.results?.rows ?? []) {
    let group = out.find((g) => g.origin === row.origin);
    if (!group) out.push((group = { origin: row.origin, count: 0, files: [] }));
    let file = group.files.find((f) => f.rel === row.rel);
    if (!file) group.files.push((file = { rel: row.rel, rows: [] }));
    file.rows.push(row);
    group.count++;
  }
  return out;
});
const fileCount = computed(() => groups.value.reduce((sum, group) => sum + group.files.length, 0));
// a reference comes without its line; the lines are read for those
const missing = computed(() => (ide.results?.rows ?? []).filter((row) => !row.preview));
const { data: lines } = useQuery(() =>
  linesQuery({
    id: workspaceId,
    places: missing.value.map((row) => ({ path: row.path, line: row.line, col: row.col, definition: false })),
    live: true,
  }),
);
/** A result's line split around its match. */
function parts(row: Result): [string, string, string] {
  const i = row.preview ? -1 : missing.value.indexOf(row);
  const text = row.preview || (lines.value?.[i]?.[0] ?? "");
  const { start, end } = editorRange({ start: { line: 0, character: row.col }, end: { line: 0, character: row.end } }, () => text);
  return [text.slice(0, start.character).trimStart(), text.slice(start.character, end.character), text.slice(end.character)];
}

function open(at: { path: string; line: number; col: number }): void {
  ide.open(at.path, { line: at.line, col: at.col });
}
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col bg-muted text-(length:--ide-ui-font)">
    <div class="flex h-8 shrink-0 items-center gap-0.5 border-b border-default ps-2 pe-1.5">
      <UTabs :model-value="ide.panel" :items="TABS" :content="false" variant="link" size="sm" @update:model-value="ide.panel = isTab($event)">
        <template #trailing="{ item }">
          <template v-if="item.value === 'problems'">
            <UBadge v-if="counts.errors" :label="String(counts.errors)" color="error" size="sm" />
            <UBadge v-if="counts.warnings" :label="String(counts.warnings)" color="warning" size="sm" />
          </template>
        </template>
      </UTabs>
      <span class="grow" />
      <template v-if="ide.panel === 'problems'">
        <UInput v-model="filter" placeholder="Filter problems" size="xs" class="w-50" icon="i-lucide-filter" />
        <UButton label="Open in Health" variant="ghost" size="xs" :to="{ name: 'health', params: { id: workspaceId }, query: { card: 'script' } }" />
      </template>
      <template v-if="isMain">
        <UTooltip text="Open in its own window">
          <UButton icon="i-lucide-app-window" variant="ghost" size="sm" square aria-label="Open in its own window" @click="openWindow(`/workspace/${workspaceId}`, 'panel', 'Panel')" />
        </UTooltip>
        <UButton icon="i-lucide-x" variant="ghost" size="sm" square aria-label="Close the panel" @click="ide.panel = ''" />
      </template>
      <UTooltip v-else text="Back into the main window">
        <UButton icon="i-lucide-x" variant="ghost" size="sm" square aria-label="Back into the main window" @click="Close(windowName)" />
      </UTooltip>
    </div>
    <div v-if="ide.panel === 'problems'" class="min-h-0 flex-1 overflow-y-auto py-1">
      <template v-for="file in files" :key="file.path">
        <div class="flex h-6 items-center gap-1.5 px-3 text-12 font-medium whitespace-nowrap text-highlighted">
          <OriginDot :color="store.originColor(file.origin)" small />
          <span class="truncate">{{ file.rel }}</span>
          <span class="text-11 font-normal text-dimmed">{{ file.found.length }}</span>
        </div>
        <button
          v-for="(one, i) in file.found"
          :key="i"
          class="flex h-6 w-full items-center gap-2 ps-7.5 pe-3 text-start text-12 whitespace-nowrap text-toned hover:bg-accented"
          @click="open({ path: file.path, line: one.range.start.line, col: one.range.start.character })"
        >
          <UIcon
            :name="one.severity === 1 ? 'i-lucide-circle-x' : 'i-lucide-triangle-alert'"
            class="size-3.5 shrink-0"
            :class="one.severity === 1 ? 'text-error' : 'text-warning'"
          />
          <span class="min-w-0 grow truncate">{{ one.message }}</span>
          <span class="shrink-0 font-mono text-11 text-dimmed">{{ one.code }}</span>
          <span class="shrink-0 font-mono text-11 text-dimmed">{{ one.range.start.line + 1 }}:{{ one.range.start.character + 1 }}</span>
        </button>
      </template>
      <p v-if="!files.length" class="px-3 py-2 text-12 text-dimmed">
        {{ filter ? "No problem matches the filter." : "No problems in the open files." }}
      </p>
    </div>
    <div v-else-if="ide.panel === 'git'" ref="gitLog" class="min-h-0 flex-1 overflow-y-auto px-3 py-1.5 font-mono text-11">
      <div v-for="(line, i) in git.output" :key="i" class="pb-1.5">
        <div :class="line.failed ? 'text-error' : 'text-dimmed'">&gt; {{ line.command }}</div>
        <pre v-if="line.output" class="whitespace-pre-wrap text-toned">{{ line.output }}</pre>
      </div>
      <p v-if="!git.output.length" class="font-sans text-12 text-dimmed">The git commands source control runs show here.</p>
    </div>
    <div v-else class="min-h-0 flex-1 overflow-y-auto py-1">
      <p v-if="ide.results" class="flex items-center gap-2 px-3 pb-1 text-11 text-dimmed">
        {{ ide.results.title }}: {{ plural(ide.results.rows.length, "result") }} in {{ plural(fileCount, "file") }}
        <span v-if="!ide.results.done">· searching…</span>
        <UBadge v-if="ide.results.capped" label="Stopped at the cap" color="warning" size="sm" />
      </p>
      <p v-else class="px-3 py-2 text-12 text-dimmed">Search from the sidebar, or Find all references (Shift Alt F12).</p>
      <template v-for="group in groups" :key="group.origin">
        <div class="flex h-6 items-center gap-1.5 px-3 text-12 font-medium text-highlighted">
          <OriginDot :color="store.originColor(group.origin)" small />
          {{ store.originName(group.origin) }}
          <UBadge :label="String(group.count)" size="sm" />
        </div>
        <template v-for="file in group.files" :key="file.rel">
          <div class="flex h-5.5 items-center ps-6.5 pe-3 font-mono text-11 text-toned"><span class="truncate">{{ file.rel }}</span></div>
          <button
            v-for="(row, i) in file.rows"
            :key="i"
            class="flex h-5.5 w-full items-center gap-2 ps-10 pe-3 text-start font-mono text-11 whitespace-nowrap text-toned hover:bg-accented"
            @click="open(row)"
          >
            <span class="w-8.5 shrink-0 text-end text-dimmed">{{ row.line + 1 }}</span>
            <span class="truncate">{{ parts(row)[0] }}<b class="rounded-xs bg-(--accent-soft) font-medium text-highlighted">{{ parts(row)[1] }}</b>{{ parts(row)[2] }}</span>
          </button>
        </template>
      </template>
    </div>
  </div>
</template>
