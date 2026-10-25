<!-- The detail of a finding: the places it points at, with the kept one marked, and a Compare button. -->
<script setup lang="ts">
import { computed, shallowRef } from "vue";
import { useQuery } from "@pinia/colada";
import type { Finding, FindingSite } from "@services/health/models";
import { diagnosticMessage, list, plural, REASONS } from "../../lib/format";
import { codesQuery, comparedQuery, readersQuery, sourcesQuery } from "../../queries";
import { useWorkspaceStore } from "../../stores/workspace";
import CompareView from "../../ui/CompareView.vue";
import OriginChip from "../../ui/OriginChip.vue";
import ScriptSnippet from "../../ui/ScriptSnippet.vue";
import SourceLink from "../../ui/SourceLink.vue";
import { RULE_COLORS } from "./HealthTable.vue";

const { finding } = defineProps<{ finding: Finding }>();

const workspace = useWorkspaceStore();
const { data: codes } = useQuery(codesQuery);
const code = computed(() => codes.value?.find((entry) => entry.id === finding.code));
const message = computed(() => (code.value ? diagnosticMessage(code.value.message, finding.facts ?? {}) : ""));
const sites = computed(() => finding.sites ?? []);
const hasKept = computed(() => sites.value.some((site) => site.kept));
const { data: sources } = useQuery(() => ({
  ...sourcesQuery(sites.value),
  enabled: workspace.isOpen() && sites.value.length > 0,
}));
const { data: readers } = useQuery(() => ({
  ...readersQuery(finding.name),
  enabled: workspace.isOpen() && finding.section === "localization" && !!finding.name,
}));

const pair = computed(() => ({ a: sites.value[0], b: sites.value[1] }));
const comparing = shallowRef(false);
const {
  data: compared,
  error,
  isLoading,
} = useQuery(() => ({
  ...comparedQuery(pair.value),
  enabled: workspace.isOpen() && comparing.value && finding.comparable,
}));

/** Describes a place for the label over one side of the comparison: its origin and its file. */
function describeSite(site: FindingSite): string {
  return `${workspace.originName(site.place.origin)} · ${site.place.rel}`;
}
</script>

<template>
  <section class="flex min-w-0 grow flex-col bg-default">
    <div class="flex shrink-0 flex-col gap-1.5 border-b border-default px-4 pt-3.5 pb-3">
      <div class="flex flex-wrap items-center gap-2">
        <UBadge v-if="code" :label="code.label" size="sm" />
        <UBadge v-if="finding.database" :label="finding.database" variant="outline" size="sm" />
        <UBadge
          v-if="finding.facts?.rule"
          :label="finding.facts.rule"
          :color="RULE_COLORS[finding.facts.rule] ?? 'info'"
          size="sm"
        />
        <OriginChip v-if="finding.origin" :origin="finding.origin" />
        <span class="grow" />
        <UButton v-if="finding.comparable" label="Compare the two" icon="i-lucide-diff" @click="comparing = true" />
      </div>
      <span v-if="finding.name" class="font-mono text-16 leading-5.5 font-semibold break-all text-highlighted">
        {{ finding.name }}
      </span>
      <p v-if="message" class="text-12 text-muted">{{ message }}</p>
      <p v-if="finding.removed?.length" class="text-12 text-warning">
        The kept file leaves out {{ plural(finding.removed.length, "name") }}: {{ list(finding.removed) }}.
      </p>
    </div>
    <div class="flex min-h-0 flex-col gap-3 overflow-auto px-4 py-3">
      <p v-if="!sites.length" class="text-12 text-dimmed">This finding points at no place.</p>
      <div
        v-for="(site, index) in sites"
        :key="`${site.place.path}:${site.range.startLineNumber}:${site.range.startColumn}`"
        class="shrink-0 overflow-hidden rounded-lg border border-default bg-(--bg-0)"
      >
        <div class="flex h-8 items-center gap-2 border-b border-default bg-elevated px-2.5 text-12 whitespace-nowrap">
          <SourceLink :place="site.place" :line="site.range.startLineNumber" class="min-w-0" />
          <span class="grow" />
          <UBadge v-if="site.reason" :label="REASONS[site.reason] ?? site.reason" variant="outline" size="sm" />
          <UBadge v-if="site.kept && site.reason !== 'merged'" label="Kept" color="success" size="sm" />
          <UBadge v-else-if="!site.kept && hasKept" label="Replaced" size="sm" />
        </div>
        <div v-if="site.defines?.length" class="flex flex-col gap-1.5 px-2.5 py-2">
          <span class="text-11 text-dimmed">
            Defines {{ site.defines.length }}
            <template v-if="site.removed?.length">
              ; <span class="text-warning">the kept file leaves out {{ site.removed.length }}</span>
            </template>
          </span>
          <div class="flex flex-wrap gap-x-3 gap-y-0.5 font-mono text-12">
            <span v-for="name in site.defines" :key="name" :class="{ 'text-warning': site.removed?.includes(name) }">
              {{ name }}
            </span>
          </div>
        </div>
        <ScriptSnippet
          v-else-if="sources?.[index]"
          :text="sources[index]"
          language="paradox-script"
          class="px-2.5 py-1.5"
          :class="{ 'opacity-70': hasKept && !site.kept }"
        />
      </div>
      <section v-if="readers?.length" class="flex flex-col items-start gap-1">
        <h4 class="text-12 font-semibold">Read by</h4>
        <SourceLink
          v-for="reader in readers"
          :key="`${reader.place.path}:${reader.range.startLineNumber}:${reader.range.startColumn}`"
          :place="reader.place"
          :line="reader.range.startLineNumber"
        />
      </section>
    </div>
    <UModal v-model:open="comparing" title="Compare the two" :ui="{ content: 'max-w-5xl' }">
      <template #body>
        <UAlert v-if="error" color="error" variant="subtle" :description="error.message" />
        <CompareView
          v-else-if="compared"
          :left="compared.a"
          :right="compared.b"
          language="paradox-script"
          :left-label="describeSite(pair.a)"
          :right-label="describeSite(pair.b)"
          class="h-[70vh]"
        />
        <p v-else-if="isLoading" class="text-12 text-dimmed">Reading the two definitions.</p>
      </template>
    </UModal>
  </section>
</template>
