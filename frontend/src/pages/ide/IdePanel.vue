<!--
  The IDE's bottom panel. It shows the problems of the open files, the hits of the search or the output of git.
-->
<script lang="ts">
import type { Finding } from "@services/health/models";
import type { Code, Diagnostic } from "@services/lsp/models";
import type { Place } from "@services/workspace/models";

import { useEditorStore } from "../../stores/editor";
import { useSettingsStore } from "../../stores/settings";
import { findModel } from "./editor/models";

/** A Problem is one problem in the list, an editor diagnostic or a Health finding. A finding has its own level. */
export type Problem = { code: Code; problem: Diagnostic; level: string; health?: true };

/**
 * listPanelProblems lists the editor's problems and Health's findings for each file open in a tab. A file with no
 * problem is left out.
 */
export function listPanelProblems(
  codes: readonly Code[],
  findings: readonly Finding[],
): { place: Place; problems: Problem[] }[] {
  const editor = useEditorStore();
  const off = useSettingsStore().value("ide.checks").split(",");
  const places = editor.listTabs().flatMap((one) => editor.placeOf(one.id) ?? []);
  return [...new Map(places.map((place) => [place.key, place])).values()].flatMap((place) => {
    const own = (findModel(place.key)?.problems ?? []).flatMap((problem) => {
      const code = codes.find((one) => one.id === problem.code);
      return code ? [{ code, problem, level: code.level }] : [];
    });
    const problems = [...own, ...listFindings(place, codes, findings, off)];
    return problems.length ? [{ place, problems }] : [];
  });
}

/** listFindings returns a file's findings about the mods together as problems. Checks turned off are left out. */
function listFindings(place: Place, codes: readonly Code[], findings: readonly Finding[], off: string[]): Problem[] {
  return findings.flatMap((finding) => {
    const code = codes.find((one) => one.id === finding.code);
    if (code?.question !== "compatibility" || off.includes(code.id)) return [];
    return (finding.sites ?? []).flatMap(({ place: site, range }) =>
      site.key === place.key
        ? [
            {
              code,
              problem: { code: code.id, range, facts: finding.facts },
              level: finding.level,
              health: true as const,
            },
          ]
        : [],
    );
  });
}
</script>

<script setup lang="ts">
import { computed, shallowRef, useTemplateRef, watch } from "vue";
import { useQuery } from "@pinia/colada";

import { diagnosticMessage, matchQuery } from "../../lib/format";
import { codesQuery, reportQuery } from "../../queries";
import { useGitStore } from "../../stores/git";
import { useSearchStore } from "../../stores/search";
import { useWorkspaceStore } from "../../stores/workspace";
import OriginChip from "../../ui/OriginChip.vue";
import ProblemRow from "../../ui/ProblemRow.vue";
import SearchHits from "./SearchHits.vue";

const { tab } = defineProps<{ tab: "problems" | "search" | "output" }>();

/** A Row is one row of the problems list, a file or one of its problems with its message. */
type Row = { place: Place; count: number } | ({ place: Place; message: string } & Problem);

const workspace = useWorkspaceStore();
const editor = useEditorStore();
const git = useGitStore();
const search = useSearchStore();
const filter = shallowRef("");
const output = useTemplateRef("output");

const { data: codes } = useQuery(codesQuery);
// Health's report for the workspace's own load order
const { data: report } = useQuery(() => ({
  ...reportQuery(workspace.listModIds()),
  enabled: tab === "problems" && workspace.isOpen(),
}));
const rows = computed<Row[]>(() =>
  listPanelProblems(codes.value ?? [], report.value?.findings ?? []).flatMap(({ place, problems }) => {
    const shown = problems
      .map((one) => ({ place, ...one, message: diagnosticMessage(one.code.message, one.problem.facts ?? {}) }))
      .filter(({ message, code }) => matchQuery(filter.value, message, code.id, place.rel));
    return shown.length ? [{ place, count: shown.length }, ...shown] : [];
  }),
);

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
          :to="{ name: 'health', params: { id: workspace.activeId() } }"
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
          dense
          class="ps-7.5"
          :level="item.level"
          :message="item.message"
          @open="
            editor.show(
              item.place,
              { lineNumber: item.problem.range.startLineNumber, column: item.problem.range.startColumn },
              'navigate',
            )
          "
        >
          <template #trailing>
            <span class="shrink-0 font-mono text-11 text-dimmed">{{ item.code.id }}</span>
            <span class="shrink-0 font-mono text-11 text-dimmed">
              {{ item.problem.range.startLineNumber }}:{{ item.problem.range.startColumn }}
            </span>
            <UBadge v-if="item.health" label="Health" color="neutral" variant="subtle" size="sm" class="shrink-0" />
          </template>
        </ProblemRow>
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
