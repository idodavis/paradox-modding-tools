<!-- The IDE's bottom panel: the problems of the open files by file, the hits of the search and the output of git. -->
<script setup lang="ts">
import { computed, shallowRef, useTemplateRef, watch } from "vue";
import { useQuery } from "@pinia/colada";

import type { Code, Diagnostic } from "@services/lsp/models";
import type { Place } from "@services/workspace/models";

import { diagnosticMessage } from "../../lib/format";
import { codesQuery } from "../../queries";
import { useEditorStore } from "../../stores/editor";
import { useGitStore } from "../../stores/git";
import { useSearchStore } from "../../stores/search";
import { useWorkspaceStore } from "../../stores/workspace";
import OriginChip from "../../ui/OriginChip.vue";
import ProblemRow from "../../ui/ProblemRow.vue";
import SearchHits from "./SearchHits.vue";
import { listOpenProblems } from "./editor/models";

defineProps<{ tab: "problems" | "search" | "output" }>();

/** A row of the problems list: a file, or one problem of the file above it. */
type Row = { place: Place; count: number } | { place: Place; code: Code; problem: Diagnostic };

const workspace = useWorkspaceStore();
const editor = useEditorStore();
const git = useGitStore();
const search = useSearchStore();
const filter = shallowRef("");
const output = useTemplateRef("output");

const { data: codes } = useQuery(codesQuery);
const rows = computed<Row[]>(() => {
  const needle = filter.value.trim().toLowerCase();
  return listOpenProblems(editor.listTabs(), codes.value ?? []).flatMap(({ place, problems }) => {
    const shown = problems.filter(({ code, problem }) =>
      `${diagnosticMessage(code.message, problem.facts ?? {})} ${code.id} ${place.rel}`.toLowerCase().includes(needle),
    );
    return shown.length ? [{ place, count: shown.length }, ...shown.map((one) => ({ place, ...one }))] : [];
  });
});

// the Output keeps its newest line in view
watch(
  () => git.output().length,
  () => output.value?.scrollTo({ top: output.value.scrollHeight }),
  { flush: "post" },
);
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col bg-muted text-(length:--ide-ui-font)">
    <template v-if="tab === 'problems'">
      <Teleport defer to="#panel-actions">
        <UInput v-model="filter" placeholder="Filter problems" size="xs" class="w-50" icon="i-lucide-filter" />
        <UButton
          label="Open in Health"
          variant="ghost"
          size="xs"
          :to="{ name: 'health', params: { id: workspace.activeId() }, query: { section: 'script' } }"
        />
      </Teleport>
      <UScrollArea
        v-if="rows.length"
        v-slot="{ item }"
        :items="rows"
        :virtualize="{ estimateSize: 24, skipMeasurement: true }"
        class="min-h-0 flex-1 py-1"
      >
        <ProblemRow
          v-if="'code' in item"
          class="ps-7.5"
          :code="item.code"
          :facts="item.problem.facts ?? {}"
          :line="item.problem.range.startLineNumber"
          :column="item.problem.range.startColumn"
          @open="
            editor.show(
              item.place,
              { lineNumber: item.problem.range.startLineNumber, column: item.problem.range.startColumn },
              'navigate',
            )
          "
        />
        <div v-else class="flex h-6 items-center gap-1.5 px-3 text-12 font-medium whitespace-nowrap text-highlighted">
          <OriginChip :origin="item.place.origin" dot />
          <span class="truncate">{{ item.place.rel }}</span>
          <span class="text-11 font-normal text-dimmed">{{ item.count }}</span>
        </div>
      </UScrollArea>
      <p v-else class="px-3 py-2 text-12 text-dimmed">
        {{ filter ? "No problem matches the filter." : "No problems in the open files." }}
      </p>
    </template>
    <div v-else-if="tab === 'search'" class="min-h-0 flex-1 overflow-y-auto">
      <SearchHits v-if="search.current().query" />
      <p v-else class="px-3 py-2 text-12 text-dimmed">The sidebar's search lists its hits here.</p>
    </div>
    <div v-else ref="output" class="min-h-0 flex-1 overflow-y-auto px-3 py-1.5 font-mono text-11">
      <pre v-for="(line, index) in git.output()" :key="index" class="pb-0.5 whitespace-pre-wrap text-toned">{{
        line
      }}</pre>
      <p v-if="!git.output().length" class="font-sans text-12 text-dimmed">The output of git shows here.</p>
    </div>
  </div>
</template>
