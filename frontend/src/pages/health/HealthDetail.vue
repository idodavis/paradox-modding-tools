<!--
  The detail of one finding, with the code at each place it points at. Where the load order decides a contest, it
  offers the order that fixes it.
-->
<script setup lang="ts">
import { computed } from "vue";
import { useQuery } from "@pinia/colada";
import { isEqual } from "es-toolkit";
import type { Finding, FindingSite } from "@services/health/models";
import { describeCount, describeFinding, LEVELS, list, plural, TOPICS, toRepeatKey } from "../../lib/format";
import { codesQuery, comparedQuery, readersQuery, snippetsQuery } from "../../queries";
import { useWorkspaceStore } from "../../stores/workspace";
import CompareView from "../../ui/CompareView.vue";
import SourceLink from "../../ui/SourceLink.vue";
import HealthSite, { openSite } from "./HealthSite.vue";

const {
  finding,
  findings = [],
  order = [],
} = defineProps<{
  finding: Finding;
  /** The view's findings, among them the finding's repeats. */
  findings?: Finding[];
  /** The load order the finding was checked in. */
  order?: string[];
}>();
const emit = defineEmits<{ preview: [order: string[]] }>();
/** Whether the comparison of the two copies is open. */
const comparing = defineModel<boolean>("comparing", { default: false });

/** The checks about a localization key. */
const MESSAGE_CODES = ["missing-message", "duplicate-message", "shared-message"];

const workspace = useWorkspaceStore();
const { data: codes } = useQuery(codesQuery);
const code = computed(() => codes.value?.find((one) => one.id === finding.code));
const mark = computed(() => LEVELS[finding.level]);
/** Whether the finding is titled by its definition's name instead of its check. */
const named = computed(() => finding.question === "compatibility" && !!finding.database);
/** The finding and its repeats at other places. */
const repeats = computed(() => {
  const key = toRepeatKey(finding, codes.value);
  const same = findings.filter((one) => toRepeatKey(one, codes.value) === key);
  return same.length ? same : [finding];
});
const sites = computed(() => repeats.value.flatMap((one) => one.sites ?? []));
const hasKept = computed(() => sites.value.some((site) => site.kept));
const move = computed(() => describeMove(order, finding.order ?? []));
const { data: snippets } = useQuery(() => ({
  ...snippetsQuery(sites.value),
  enabled: workspace.isOpen() && sites.value.length > 0,
}));
const { data: readers } = useQuery(() => ({
  ...readersQuery(finding.facts?.key ?? finding.name),
  enabled: workspace.isOpen() && MESSAGE_CODES.includes(finding.code),
}));

const pair = computed(() => ({ a: sites.value[0], b: sites.value[1] }));
const {
  data: compared,
  error,
  isLoading,
} = useQuery(() => ({
  ...comparedQuery(pair.value),
  enabled: workspace.isOpen() && comparing.value && finding.comparable,
}));

/** describeSite returns the label of one side of the comparison. */
function describeSite(site: FindingSite): string {
  return `${workspace.originName(site.place.origin)} · ${site.place.rel}`;
}

/**
 * describeMove returns the one move that turns the current load order into the suggested one. It returns undefined
 * unless exactly one mod moves.
 */
function describeMove(
  current: string[],
  suggested: string[],
): { mod: string; before: boolean; other: string } | undefined {
  if (!suggested.length || isEqual(current, suggested)) return undefined;
  const mod = suggested.find((id) =>
    isEqual(
      current.filter((one) => one !== id),
      suggested.filter((one) => one !== id),
    ),
  );
  if (!mod) return undefined;
  const position = suggested.indexOf(mod);
  const before = position < current.indexOf(mod);
  const other = suggested[before ? position + 1 : position - 1];
  return other ? { mod, before, other } : undefined;
}
</script>

<template>
  <section class="flex h-full min-w-0 grow flex-col bg-default">
    <div class="flex shrink-0 flex-col gap-1.5 border-b border-default px-4 pt-3.5 pb-3">
      <!-- the badges and buttons wrap under the title when the pane is narrow -->
      <div class="flex flex-wrap items-center gap-x-2 gap-y-1.5">
        <span class="flex max-w-full items-center gap-2">
          <UIcon
            v-if="mark && !named"
            :name="mark.icon"
            class="size-4 shrink-0"
            :style="{ color: `var(--ui-${mark.color})` }"
          />
          <span class="text-13 font-semibold text-highlighted" :class="named ? 'font-mono break-all' : 'break-words'">
            {{ named ? finding.name : (code?.label ?? finding.code) }}
          </span>
        </span>
        <UBadge v-if="named" :label="finding.database" variant="outline" size="sm" class="shrink-0" />
        <UBadge v-else-if="code?.topic" :label="TOPICS[code.topic] ?? code.topic" size="sm" class="shrink-0" />
        <UBadge v-if="repeats.length > 1" :label="`×${describeCount(repeats.length)}`" size="sm" class="shrink-0" />
        <span class="grow" />
        <UButton
          v-if="finding.comparable"
          label="Compare the two"
          icon="i-lucide-diff"
          class="shrink-0"
          @click="comparing = true"
        />
        <UButton
          v-if="sites.length === 1"
          label="Open in the IDE"
          icon="i-lucide-code"
          class="shrink-0"
          @click="openSite(sites[0])"
        />
      </div>
      <p class="text-12 text-muted">{{ describeFinding(finding, codes) }}</p>
      <p v-if="finding.removed?.length" class="text-12 text-warning">
        The kept file leaves out {{ plural(finding.removed.length, "name") }}: {{ list(finding.removed) }}.
      </p>
    </div>
    <div class="flex min-h-0 flex-col gap-3 overflow-auto px-4 py-3">
      <p v-if="!sites.length" class="text-12 text-dimmed">This finding points at no place.</p>
      <HealthSite
        v-for="(site, index) in sites"
        :key="`${site.place.path}:${site.range.startLineNumber}:${site.range.startColumn}`"
        :site="site"
        :snippet="snippets?.[index]"
        :used="hasKept ? site.kept : undefined"
      />
      <div
        v-if="move"
        class="flex items-center gap-2.5 rounded-lg border border-primary bg-(--accent-soft) px-3 py-2.5"
      >
        <UIcon name="i-lucide-wand-sparkles" class="size-4 shrink-0 text-primary" />
        <span class="flex min-w-0 grow flex-col">
          <span class="text-12 font-medium text-highlighted">
            Try {{ workspace.originName(move.mod) }} {{ move.before ? "before" : "after" }}
            {{ workspace.originName(move.other) }}
          </span>
          <span class="text-11 text-dimmed">
            Fixes this. The preview shows what else would change hands before you keep the order.
          </span>
        </span>
        <UButton label="Try this order" @click="emit('preview', finding.order ?? [])" />
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
      <slot />
    </div>
    <UModal v-model:open="comparing" title="Compare the two" :ui="{ content: 'max-w-5xl' }">
      <template #body>
        <UAlert
          v-if="error"
          color="error"
          variant="subtle"
          title="The two definitions could not be read. Open each in the IDE from the row's menu."
          :description="error.message"
        />
        <CompareView
          v-else-if="compared && pair.a && pair.b"
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
