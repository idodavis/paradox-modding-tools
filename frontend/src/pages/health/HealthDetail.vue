<!--
  The detail of one finding: a fixed head with its title, its level and its buttons, over sections that scroll. The
  sections say what is wrong, where it is, and which copy the game uses and why.
-->
<script setup lang="ts">
import { computed, shallowRef } from "vue";
import { useQuery } from "@pinia/colada";
import { groupBy, isEqual, maxBy, sumBy } from "es-toolkit";
import type { Finding, FindingSite } from "@services/health/models";
import { compareWithGame } from "../../app/commands";
import {
  describeCount,
  LEVELS,
  list,
  matchQuery,
  type Part,
  plural,
  REASONS,
  splitFinding,
  splitMessage,
  toRepeatKey,
} from "../../lib/format";
import { codesQuery, comparedQuery, readersQuery, snippetsQuery } from "../../queries";
import { useWorkspaceStore } from "../../stores/workspace";
import CompareView from "../../ui/CompareView.vue";
import FactText from "../../ui/FactText.vue";
import ModTitle from "../../ui/ModTitle.vue";
import SourceLink from "../../ui/SourceLink.vue";
import HealthFrame from "./HealthFrame.vue";
import HealthSection from "./HealthSection.vue";
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
/** How many of the names still used show before the rest fold behind a button. */
const USES_SHOWN = 6;

const workspace = useWorkspaceStore();
const { data: codes } = useQuery(codesQuery);
const code = computed(() => codes.value?.find((one) => one.id === finding.code));
const mark = computed(() => LEVELS[finding.level]);
/**
 * Whether the finding is of a name nothing defines, the one check whose codes have an elsewhere message. Its places
 * use the name.
 */
const isMissing = computed(() => !!code.value?.elsewhere);
/** The finding and its repeats at other places. */
const repeats = computed(() => {
  const key = toRepeatKey(finding, codes.value);
  const same = findings.filter((one) => toRepeatKey(one, codes.value) === key);
  return same.length ? same : [finding];
});
/** The line under the title, after the level and the check: what the name is, and how often it is used or found. */
const subtitle = computed(() => {
  const count = repeats.value.length;
  const frequency = isMissing.value ? `used ${describeCount(count)} times` : plural(count, "place");
  return [finding.database, count > 1 ? frequency : ""].filter(Boolean).join(" · ");
});
/** The places drawn as code: the first repeat's. The other repeats' places are listed as lines. */
const sites = computed(() => repeats.value[0]?.sites ?? []);
/** The file the finding is about, or undefined for a finding of a name. */
const file = computed(() => (finding.database === "file" ? sites.value[0]?.place : undefined));
const copies = computed(() => sites.value.filter((site) => site.defines?.length));
const places = computed(() => sites.value.filter((site) => !site.defines?.length));
const kept = computed(() => places.value.filter((site) => site.kept));
const ignored = computed(() => places.value.filter((site) => !site.kept));
/** Whether the game uses one of a file's copies, so each copy says whether it is used. */
const hasKeptCopy = computed(() => copies.value.some((copy) => copy.kept));
/** The other repeats' places by file, each file named "this file" where it is the first place's, else by its path. */
const others = computed(() =>
  Object.values(
    groupBy(
      repeats.value.slice(1).flatMap((one) => one.sites ?? []),
      (site) => site.place.path,
    ),
  ).map((group) => ({
    file: group[0]?.place.path === places.value[0]?.place.path ? "this file" : (group[0]?.place.rel ?? ""),
    sites: group,
  })),
);
/** The mod's copy the game reads, where the finding is of a file that replaces the game's and removes names. */
const replacing = computed(() =>
  finding.code === "removed-names" ? sites.value.find((site) => site.kept) : undefined,
);
const move = computed(() => describeMove(order, finding.order ?? []));
/**
 * Why the game keeps the winner's copy of a contested name, as parts: the long form of the winner's reason, filled
 * with the folder and the files of the copies. It is empty for a file, a finding with no winner, and a reason with
 * no long form.
 */
const why = computed((): Part[] => {
  const winner = kept.value.find((site) => REASONS[site.reason ?? ""]?.why);
  const template = REASONS[winner?.reason ?? ""]?.why;
  if (!winner || !template || !finding.facts?.winner || finding.database === "file") return [];
  const folder = winner.place.folder;
  const beaten = ignored.value.filter((site) => !site.place.readOnly);
  return splitMessage(template, {
    ...finding.facts,
    folder: `${folder}/`,
    file: winner.place.name,
    beatenFile: list(beaten.map((site) => (site.place.folder === folder ? site.place.name : site.place.rel))),
  });
});

/** The text the names still used must hold to be listed, and whether all of them show. */
const usesFilter = shallowRef("");
const allUses = shallowRef(false);
const uses = computed(() => (finding.uses ?? []).filter((use) => matchQuery(usesFilter.value, use.name)));
const shownUses = computed(() => (allUses.value || usesFilter.value ? uses.value : uses.value.slice(0, USES_SHOWN)));
const mostUses = computed(() => maxBy(finding.uses ?? [], (use) => use.count)?.count ?? 1);
const hiddenUses = computed(() => uses.value.slice(shownUses.value.length));
/** The names the kept file leaves out that are not among the names still used. */
const unused = computed(() => (finding.removed ?? []).filter((one) => !finding.uses?.some((use) => use.name === one)));

const { data: snippets } = useQuery(() => ({
  ...snippetsQuery(places.value),
  enabled: workspace.isOpen() && places.value.length > 0,
}));
const { data: definitions } = useQuery(() => ({
  ...snippetsQuery(finding.defined ?? []),
  enabled: workspace.isOpen() && !!finding.defined?.length,
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
  <HealthFrame :icon="mark?.icon ?? 'i-lucide-info'" :label="mark?.label ?? ''" :color="mark?.color">
    <template #title>
      <!-- a file's folder is dimmed, and the title breaks after its slashes -->
      <span :title="finding.name" :class="{ 'font-mono': finding.name }">
        <template v-if="file">
          <span class="font-normal text-dimmed"
            ><template v-for="(folder, index) in file.folder ? file.folder.split('/') : []" :key="index"
              >{{ folder }}/<wbr /></template></span
          >{{ file.name }}
        </template>
        <template v-else>{{ finding.name || code?.label || finding.code }}</template>
      </span>
    </template>
    <template #line>
      <span v-if="finding.name" class="whitespace-nowrap text-toned">{{ code?.label }}</span>
      <span v-if="subtitle" class="whitespace-nowrap">· {{ subtitle }}</span>
    </template>
    <template #buttons>
      <UButton v-if="finding.comparable" label="Compare the two" icon="i-lucide-diff" @click="comparing = true" />
      <UButton
        v-if="replacing"
        label="Compare with the game's copy"
        icon="i-lucide-diff"
        @click="replacing && compareWithGame(replacing.place)"
      />
      <UButton
        v-if="places.length === 1 && !copies.length"
        label="Open in the IDE"
        icon="i-lucide-code"
        @click="places[0] && openSite(places[0])"
      />
    </template>
    <HealthSection title="What's wrong">
      <p class="max-w-[74ch] text-13 leading-5.5 text-toned"><FactText :parts="splitFinding(finding, codes)" /></p>
      <p v-if="!sites.length" class="text-12 text-dimmed">This finding points at no place.</p>
    </HealthSection>
    <HealthSection v-if="why.length" :title="`Why ${finding.facts?.winner}'s copy wins`">
      <p class="max-w-[74ch] text-13 leading-5.5 text-toned"><FactText :parts="why" /></p>
    </HealthSection>
    <HealthSection v-if="move" title="A load order that fixes it">
      <template #actions>
        <UButton label="Try this order" @click="emit('preview', finding.order ?? [])" />
      </template>
      <p class="max-w-[74ch] text-13 leading-5.5 text-toned">
        Put <b class="font-semibold text-highlighted">{{ workspace.originName(move.mod) }}</b>
        {{ move.before ? "before" : "after" }} {{ workspace.originName(move.other) }}. The preview shows what else would
        change hands before you keep the order.
      </p>
    </HealthSection>
    <HealthSection
      v-if="finding.defined?.length"
      title="Where it's defined"
      @open="finding.defined[0] && openSite(finding.defined[0])"
    >
      <HealthSite
        v-for="(site, index) in finding.defined"
        :key="`${site.place.path}:${site.range.startLineNumber}`"
        :site="site"
        :snippet="definitions?.[index]"
      />
    </HealthSection>
    <HealthSection
      v-if="places.length === 1"
      :title="isMissing ? `Where it's used` : 'Where it is'"
      :count="repeats.length > 1 ? repeats.length : undefined"
      @open="places[0] && openSite(places[0])"
    >
      <HealthSite v-for="site in places" :key="site.place.path" :site="site" :snippet="snippets?.[0]" />
      <div v-for="group in others" :key="group.file" class="flex flex-wrap items-center gap-1 text-12 text-dimmed">
        <span class="me-1">Also in {{ group.file }} at {{ group.sites.length === 1 ? "line" : "lines" }}</span>
        <button
          v-for="site in group.sites"
          :key="`${site.range.startLineNumber}:${site.range.startColumn}`"
          type="button"
          class="rounded-sm bg-elevated px-1.5 font-mono text-11.5 text-toned hover:text-highlighted"
          @click="openSite(site)"
        >
          {{ site.range.startLineNumber }}
        </button>
      </div>
    </HealthSection>
    <template v-else-if="kept.length">
      <HealthSection
        :title="kept.length === 1 ? 'The copy the game uses' : 'The copies the game uses'"
        :count="kept.length > 1 ? kept.length : undefined"
        @open="kept[0] && openSite(kept[0])"
      >
        <HealthSite
          v-for="site in kept"
          :key="`${site.place.path}:${site.range.startLineNumber}`"
          :site="site"
          :snippet="snippets?.[places.indexOf(site)]"
        />
      </HealthSection>
      <HealthSection
        v-if="ignored.length"
        :title="ignored.length === 1 ? 'The copy it ignores' : 'The copies it ignores'"
        :count="ignored.length > 1 ? ignored.length : undefined"
        @open="ignored[0] && openSite(ignored[0])"
      >
        <HealthSite
          v-for="site in ignored"
          :key="`${site.place.path}:${site.range.startLineNumber}`"
          :site="site"
          :snippet="snippets?.[places.indexOf(site)]"
          ignored
        />
      </HealthSection>
    </template>
    <HealthSection
      v-else-if="places.length"
      title="The copies"
      :count="places.length"
      @open="places[0] && openSite(places[0])"
    >
      <HealthSite
        v-for="(site, index) in places"
        :key="`${site.place.path}:${site.range.startLineNumber}`"
        :site="site"
        :snippet="snippets?.[index]"
      />
    </HealthSection>
    <HealthSection v-if="finding.uses?.length" title="Still used" :count="finding.uses.length">
      <template #actions>
        <UInput
          v-model="usesFilter"
          icon="i-lucide-filter"
          size="xs"
          placeholder="Filter names"
          class="w-44 font-normal"
        />
      </template>
      <div class="flex flex-col">
        <div
          v-for="use in shownUses"
          :key="use.name"
          class="flex min-h-9.5 items-center gap-3 border-t border-default py-1.5 first:border-t-0"
        >
          <span class="flex min-w-0 grow flex-col items-start">
            <span class="max-w-full truncate font-mono text-12 text-highlighted">{{ use.name }}</span>
            <button
              type="button"
              class="max-w-full truncate text-start font-mono text-11 text-dimmed hover:underline"
              @click="openSite(use.first)"
            >
              first used in {{ use.first.place.rel }}:{{ use.first.range.startLineNumber }}
            </button>
          </span>
          <span class="flex shrink-0 items-center gap-2 text-12 font-medium text-highlighted tabular-nums">
            <span
              class="h-1 rounded-full bg-error opacity-60"
              :style="{ width: `${Math.max(2, (use.count / mostUses) * 40)}px` }"
            />
            {{ plural(use.count, "use") }}
          </span>
        </div>
        <button
          v-if="hiddenUses.length"
          type="button"
          class="flex min-h-8 items-center gap-2 border-t border-default text-12 text-dimmed hover:text-highlighted"
          @click="allUses = true"
        >
          <UIcon name="i-lucide-chevron-down" class="size-3.5" />
          Show {{ describeCount(hiddenUses.length) }} more, used
          {{
            plural(
              sumBy(hiddenUses, (use) => use.count),
              "time",
            )
          }}
        </button>
        <p v-if="!uses.length" class="text-12 text-dimmed">No name matches.</p>
      </div>
    </HealthSection>
    <HealthSection
      v-if="finding.uses?.length && unused.length"
      title="The other names it leaves out"
      :count="unused.length"
      fold
    >
      <div class="flex flex-wrap gap-x-3 gap-y-0.5 font-mono text-12 text-toned">
        <span v-for="one in unused" :key="one">{{ one }}</span>
      </div>
    </HealthSection>
    <HealthSection
      v-if="copies.length"
      :title="copies.length === 2 ? 'The two copies' : 'The copies'"
      :count="copies.length === 2 ? undefined : copies.length"
    >
      <div class="flex flex-col">
        <button
          v-for="copy in copies"
          :key="copy.place.path"
          type="button"
          class="-mx-2 flex min-h-9.5 items-center gap-2.5 rounded-md border-t border-default px-2 py-1.5 text-start text-12 first:border-t-0 hover:bg-accented"
          :title="`Open ${copy.place.rel} in the IDE`"
          @click="openSite(copy)"
        >
          <ModTitle :origin="copy.place.origin" />
          <span class="shrink-0 text-dimmed">defines {{ describeCount(copy.defines?.length ?? 0) }}</span>
          <span class="grow" />
          <span v-if="copy.reason" class="truncate text-dimmed">{{ REASONS[copy.reason]?.label ?? copy.reason }}</span>
          <UBadge
            v-if="hasKeptCopy"
            :label="copy.kept ? 'Used' : 'Not used'"
            :color="copy.kept ? 'success' : 'neutral'"
            size="sm"
            class="shrink-0"
          />
        </button>
      </div>
    </HealthSection>
    <HealthSection v-if="readers?.length" title="Where it's read" :count="readers.length">
      <div class="flex flex-col items-start gap-1">
        <SourceLink
          v-for="reader in readers"
          :key="`${reader.place.path}:${reader.range.startLineNumber}:${reader.range.startColumn}`"
          :place="reader.place"
          :line="reader.range.startLineNumber"
        />
      </div>
    </HealthSection>
    <slot />
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
  </HealthFrame>
</template>
