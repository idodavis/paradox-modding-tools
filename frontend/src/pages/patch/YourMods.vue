<!--
  Your mods: what the game's patches changed under each mod. Three panels say
  it for all the mods, a list ranks the mods worst first, and the mod chosen
  shows its renamed names, the game files its copies replace and the notes
  that name what it holds.
-->
<script setup lang="ts">
import { computed, reactive, shallowRef } from "vue";
import { useMutation, useQuery, useQueryCache } from "@pinia/colada";
import { groupBy, sortBy, sumBy, uniq } from "es-toolkit";

import type { Impact } from "@services/patch/models";
import * as patch from "@services/patch/service";
import type { Place } from "@services/workspace/models";
import * as workspaceApi from "@services/workspace/service";
import { compareWithGame } from "../../app/commands";
import { toast } from "../../lib/feedback";
import { describeCount, describeRelease, list, plural } from "../../lib/format";
import { keys, levelsQuery, notesQuery } from "../../queries";
import { useEditorStore } from "../../stores/editor";
import { useWorkspaceStore } from "../../stores/workspace";
import LevelMeter from "../../ui/LevelMeter.vue";
import OriginChip from "../../ui/OriginChip.vue";
import SourceLink from "../../ui/SourceLink.vue";
import VerdictHead from "../../ui/VerdictHead.vue";
import ImpactBlock from "./ImpactBlock.vue";

const { impacts } = defineProps<{ impacts: Impact[] }>();

/** How many rows a block lists before it offers the rest. */
const ROWS_SHOWN = 5;
/** The blocks of names, each of the findings of one check of a card. */
const NAME_BLOCKS = [
  { key: "renamed", title: "Names the game renamed" },
  { key: "removed", title: "Names the game removed" },
] as const;

const workspace = useWorkspaceStore();
const editor = useEditorStore();
const cache = useQueryCache();
const { data: levels } = useQuery(levelsQuery);
const { data: notes } = useQuery(() => ({ ...notesQuery, enabled: workspace.isOpen() }));

/** The releases with known changes, newest first. */
const releases = computed(() =>
  (notes.value ?? []).filter((note) => note.known && !note.beta).map((note) => note.version),
);
const expanded = reactive(new Set<string>());
const chosen = shallowRef("");

/** The mods, each with what the patches changed under it. */
const cards = computed(() =>
  impacts.map((impact) => {
    const findings = groupBy(impact.findings ?? [], (finding) => finding.code);
    const ties = (impact.ties ?? []).filter((tie) => tie.article.breaking || tie.article.new);
    const renamed = findings["patch-renamed"] ?? [];
    const removed = findings["patch-removed"] ?? [];
    const files = findings["stale-copy"] ?? [];
    const breaking = ties.filter((tie) => tie.article.breaking);
    const added = ties.filter((tie) => tie.article.new);
    const counts = [
      { word: "renamed", count: renamed.length, color: levelColor(renamed[0]?.level) },
      { word: "removed", count: removed.length, color: levelColor(removed[0]?.level) },
      { word: files.length === 1 ? "file" : "files", count: files.length, color: levelColor(files[0]?.level) },
      { word: "breaking", count: breaking.length, color: "error" },
      { word: "new", count: added.length, color: "success" },
    ].filter((row) => row.count);
    const mod = workspace.listMods().find((own) => own.id === impact.modId);
    return { impact, mod, renamed, removed, files, breaking, added, counts };
  }),
);
type Card = (typeof cards.value)[number];

const ranked = computed(() =>
  sortBy(cards.value, [
    (card) => -(card.renamed.length + card.removed.length + card.breaking.length),
    (card) => -card.files.length,
    (card) => -card.added.length,
  ]),
);
const listed = computed(() => ranked.value.filter((card) => !isQuiet(card)));
const quiet = computed(() => ranked.value.filter(isQuiet));
const current = computed(
  () => ranked.value.find((card) => card.impact.modId === chosen.value) ?? listed.value[0] ?? ranked.value[0],
);
/** The blocks of notes that name what the chosen mod holds. */
const tieBlocks = computed(() => [
  {
    key: "breaking",
    title: "Breaking changes in what it holds",
    icon: "i-lucide-triangle-alert",
    tone: "error",
    ties: current.value?.breaking ?? [],
  },
  {
    key: "new",
    title: "New features it can use",
    icon: "i-lucide-star",
    tone: "success",
    ties: current.value?.added ?? [],
  },
]);
const busiest = computed(() => Math.max(1, ...cards.value.map((card) => sumBy(card.counts, (row) => row.count))));
const panels = computed(() => {
  const names = cards.value.flatMap((card) => [...card.renamed, ...card.removed]);
  const withNames = cards.value.filter((card) => card.renamed.length + card.removed.length);
  const withFiles = cards.value.filter((card) => card.files.length);
  const mergeable = sumBy(cards.value, (card) => card.files.filter(canMerge).length);
  const breaking = sumBy(cards.value, (card) => card.breaking.length);
  const added = sumBy(cards.value, (card) => card.added.length);
  const versions = cards.value.flatMap((card) => [...card.breaking, ...card.added].map((tie) => tie.version));
  const shownNames = uniq(names.map((finding) => finding.name));
  const nameMore = shownNames.length > 2 ? [`${describeCount(shownNames.length - 2)} more`] : [];
  return [
    {
      icon: "i-lucide-tag",
      kicker: "Names the game changed",
      color: names.length ? "error" : "success",
      verdict: describeVerdict(
        [
          { count: sumBy(cards.value, (card) => card.renamed.length), word: "renamed" },
          { count: sumBy(cards.value, (card) => card.removed.length), word: "removed" },
        ],
        "Nothing renamed or removed",
      ),
      note: [
        list([...shownNames.slice(0, 2), ...nameMore]),
        list(withNames.map((card) => workspace.originName(card.impact.modId))),
        list(uniq(names.map((finding) => finding.facts?.version ?? ""))),
      ]
        .filter(Boolean)
        .join(" · "),
    },
    {
      icon: "i-lucide-file",
      kicker: "Game files under your copies",
      color: withFiles.length ? levelColor(withFiles[0]?.files[0]?.level) : "success",
      verdict: describeVerdict(
        [{ count: sumBy(withFiles, (card) => card.files.length), word: "changed" }],
        "Nothing changed",
      ),
      note: [
        ...withFiles.map((card) => `${workspace.originName(card.impact.modId)} ${describeCount(card.files.length)}`),
        mergeable && `${describeCount(mergeable)} can merge`,
      ]
        .filter(Boolean)
        .join(" · "),
    },
    {
      icon: "i-lucide-newspaper",
      kicker: "Patch notes about your mods",
      color: breaking ? "error" : "success",
      verdict: describeVerdict(
        [
          { count: breaking, word: "breaking", tone: "text-error" },
          { count: added, word: "new", tone: "text-success" },
        ],
        "Nothing names your mods",
      ),
      note: versions.length ? `From ${list(uniq(versions))}, naming what your mods write` : "",
    },
  ];
});

const refresh = () =>
  Promise.all([cache.invalidateQueries({ key: keys.patch }), cache.invalidateQueries({ key: keys.health })]);
const { mutate: setBase } = useMutation({
  mutation: ({ modId, base }: { modId: string; base: string }) =>
    workspace.updateMod(workspace.activeId(), modId, { base }),
  onSuccess: refresh,
});
const { mutate: markMadeFor } = useMutation({
  mutation: (modId: string) => patch.UpdateSupportedVersion(modId),
  onSuccess: refresh,
});
const { mutate: merge } = useMutation({
  mutation: (place: Place) => workspaceApi.MergeGameFile(place.path),
  onSuccess: (unfit, place) => {
    editor.show(place);
    toast?.add({
      title: "Merged the game's changes",
      description: unfit
        ? `${plural(unfit, "change")} of the game's did not fit. Compare with the game's to see ${unfit === 1 ? "it" : "them"}.`
        : "All of the game's changes fit.",
    });
  },
});

/** levelColor returns the Nuxt UI color of a level, or info if unknown. */
function levelColor(level: string | undefined): string {
  return levels.value?.find((row) => row.id === level)?.color ?? "info";
}

/**
 * isQuiet reports whether a mod is made for the game's release and nothing the
 * patches changed is in it.
 */
function isQuiet({ impact, counts }: Card): boolean {
  return !counts.length && !!impact.start && describeRelease(impact.start) === describeRelease(impact.end);
}

/** canMerge reports whether the game's changes to a file can be merged. */
function canMerge(file: Card["files"][number]): boolean {
  return file.facts?.status === "modified" && !!file.facts.base;
}

/**
 * describeVerdict returns the parts of a panel's headline, one for each count
 * that is not 0. With none, it returns the given words.
 */
function describeVerdict(
  parts: { count: number; word: string; tone?: string }[],
  none: string,
): { text: string; tone?: string }[] {
  const said = parts.filter((part) => part.count);
  return said.length
    ? said.map((part) => ({ text: `${describeCount(part.count)} ${part.word}`, tone: part.tone }))
    : [{ text: none }];
}

/** describeRange returns the versions a mod's patches run from and to. */
function describeRange({ impact, mod }: Card): string {
  const start = impact.start || (impact.judged && mod?.descriptor.supportedVersion) || "no version";
  return `${start} → ${impact.end}`;
}

/** describeEmpty says why a mod has nothing listed. */
function describeEmpty({ impact, mod }: Card): string {
  if (impact.start) return "Nothing the patches changed is in this mod.";
  if (!impact.judged) return "Pick the version this mod is made for, and PMT shows what the patches changed under it.";
  return `PMT knows no patch between ${mod?.descriptor.supportedVersion} and ${impact.end} yet.`;
}

/** listShown returns a block's rows: all once asked for, else the first few. */
function listShown<Row>(key: string, rows: Row[]): Row[] {
  return expanded.has(key) ? rows : rows.slice(0, ROWS_SHOWN);
}

/** hasMore reports whether a block has rows it has not listed. */
function hasMore(key: string, total: number): boolean {
  return !expanded.has(key) && total > ROWS_SHOWN;
}
</script>

<template>
  <div class="flex min-h-0 flex-col gap-3.5 px-6 py-4">
    <p v-if="!impacts.length" class="text-13 text-dimmed">This workspace has no mods.</p>
    <template v-else>
      <div class="grid shrink-0 grid-cols-[repeat(auto-fit,minmax(17rem,1fr))] gap-3">
        <section
          v-for="panel in panels"
          :key="panel.kicker"
          class="min-w-0 rounded-lg border border-default bg-muted px-4 py-3.5"
        >
          <VerdictHead :color="panel.color" :icon="panel.icon" :kicker="panel.kicker" :note="panel.note">
            <template v-for="(part, position) in panel.verdict" :key="position">
              <template v-if="position"> · </template>
              <span :class="part.tone">{{ part.text }}</span>
            </template>
          </VerdictHead>
        </section>
      </div>

      <div class="flex min-h-0 flex-1 overflow-hidden rounded-lg border border-default">
        <section class="flex w-[34%] min-w-64 shrink-0 flex-col overflow-y-auto border-e border-default bg-muted">
          <div class="flex h-9 shrink-0 items-center gap-2 border-b border-default px-3 text-12 font-semibold">
            Mods<span class="text-11 font-normal text-dimmed">{{ cards.length }}</span>
            <span class="grow" />
            <span class="text-11 font-normal text-dimmed">worst first</span>
          </div>
          <button
            v-for="card in listed"
            :key="card.impact.modId"
            type="button"
            class="flex shrink-0 flex-col gap-1.5 border-b border-default border-s-2 px-3 py-2.5 text-start hover:bg-accented"
            :class="card === current ? 'border-s-primary bg-(--accent-soft)' : 'border-s-transparent'"
            @click="chosen = card.impact.modId"
          >
            <span class="flex min-w-0 items-center gap-2">
              <OriginChip :origin="card.impact.modId" dot />
              <span class="min-w-0 truncate text-13 font-semibold text-highlighted">
                {{ workspace.originName(card.impact.modId) }}
              </span>
              <span class="grow" />
              <span class="font-mono text-11.5 whitespace-nowrap text-dimmed">{{ describeRange(card) }}</span>
            </span>
            <LevelMeter :segments="card.counts" :scale="busiest" />
            <span class="flex items-center gap-2.5 text-11.5 whitespace-nowrap text-dimmed">
              <span v-for="row in card.counts" :key="row.word">
                <b class="font-semibold" :style="{ color: `var(--ui-${row.color})` }">{{ describeCount(row.count) }}</b>
                {{ row.word }}
              </span>
              <span v-if="!card.counts.length" class="truncate" :title="describeEmpty(card)">
                {{ describeEmpty(card) }}
              </span>
            </span>
          </button>
          <template v-if="quiet.length">
            <div class="flex h-7.5 shrink-0 items-center border-b border-default bg-elevated px-3 text-11 text-dimmed">
              Made for {{ describeRelease(quiet[0]?.impact.end ?? "") }}.*, nothing to do
            </div>
            <button
              v-for="card in quiet"
              :key="card.impact.modId"
              type="button"
              class="flex shrink-0 items-center gap-2 border-b border-default border-s-2 px-3 py-2.5 text-start opacity-70 hover:bg-accented"
              :class="card === current ? 'border-s-primary bg-(--accent-soft)' : 'border-s-transparent'"
              @click="chosen = card.impact.modId"
            >
              <OriginChip :origin="card.impact.modId" dot />
              <span class="min-w-0 truncate text-13 font-medium text-highlighted">
                {{ workspace.originName(card.impact.modId) }}
              </span>
              <span class="grow" />
              <span class="font-mono text-11.5 whitespace-nowrap text-dimmed">{{ describeRange(card) }}</span>
            </button>
          </template>
        </section>

        <section v-if="current" class="flex min-w-0 grow flex-col bg-(--bg-0)">
          <div class="flex h-13 shrink-0 items-center gap-2.5 border-b border-default bg-muted px-4">
            <OriginChip :origin="current.impact.modId" dot />
            <span class="min-w-0 truncate text-16">{{ workspace.originName(current.impact.modId) }}</span>
            <span class="font-mono text-12 whitespace-nowrap text-dimmed">{{ describeRange(current) }}</span>
            <span class="grow" />
            <UButton
              label="Open in the IDE"
              icon="i-lucide-code"
              variant="ghost"
              :to="{ name: 'workspace-ide', params: { id: workspace.activeId() } }"
            />
          </div>
          <div
            v-if="!current.impact.judged || !current.mod?.workshop"
            class="flex h-10 shrink-0 items-center gap-2 border-b border-default bg-elevated px-4 text-12 text-dimmed"
          >
            <template v-if="!current.impact.judged">
              <span>No version given, so PMT compares from</span>
              <USelectMenu
                :model-value="current.mod?.base || undefined"
                :items="releases"
                :search-input="false"
                placeholder="Pick a version"
                size="xs"
                class="w-32"
                @update:model-value="setBase({ modId: current.impact.modId, base: `${$event}` })"
              />
            </template>
            <span class="grow" />
            <UButton
              v-if="!current.mod?.workshop"
              :label="`Mark as made for ${current.impact.end}`"
              variant="subtle"
              size="sm"
              @click="markMadeFor(current.impact.modId)"
            />
          </div>

          <p v-if="!current.counts.length" class="px-4 py-3.5 text-13 text-dimmed">{{ describeEmpty(current) }}</p>
          <div v-else class="@container min-h-0 flex-1 overflow-y-auto p-4">
            <div class="grid grid-cols-1 items-start gap-3.5 @3xl:grid-cols-[minmax(0,1.25fr)_minmax(0,1fr)]">
              <div class="flex min-w-0 flex-col gap-3.5">
                <template v-for="{ key, title } in NAME_BLOCKS" :key="key">
                  <ImpactBlock
                    v-if="current[key].length"
                    tone="error"
                    icon="i-lucide-tag"
                    :title="title"
                    :count="current[key].length"
                  >
                    <div
                      v-for="finding in current[key]"
                      :key="finding.name"
                      class="flex h-7.5 items-center gap-2.5 border-t border-default px-3 text-12"
                    >
                      <code
                        class="font-mono"
                        :class="
                          finding.facts?.replacement ? 'text-toned line-through decoration-error' : 'text-highlighted'
                        "
                        >{{ finding.name }}</code
                      >
                      <template v-if="finding.facts?.replacement">
                        <UIcon name="i-lucide-arrow-right" class="size-3.5 shrink-0 text-dimmed" />
                        <code class="font-mono text-(--syn-reference)">{{ finding.facts.replacement }}</code>
                      </template>
                      <span class="grow" />
                      <span v-if="finding.count > 1" class="text-11 whitespace-nowrap text-dimmed">
                        +{{ describeCount(finding.count - 1) }} more
                      </span>
                      <SourceLink
                        v-if="finding.sites?.[0]"
                        :place="finding.sites[0].place"
                        :line="finding.sites[0].line"
                        short
                        plain
                        class="max-w-75"
                      />
                      <span class="font-mono text-11 text-dimmed">{{ finding.facts?.version }}</span>
                    </div>
                  </ImpactBlock>
                </template>

                <ImpactBlock
                  v-if="current.files.length"
                  tone="warning"
                  icon="i-lucide-file"
                  title="Game files your copies replace"
                  :count="current.files.length"
                  :aside="current.files.some(canMerge) ? `${current.files.filter(canMerge).length} can merge` : ''"
                  :more="
                    hasMore(`${current.impact.modId}:files`, current.files.length)
                      ? plural(current.files.length - ROWS_SHOWN, 'more file')
                      : ''
                  "
                  @more="expanded.add(`${current.impact.modId}:files`)"
                >
                  <div
                    v-for="file in listShown(`${current.impact.modId}:files`, current.files)"
                    :key="file.name"
                    class="flex h-7.5 items-center gap-2.5 border-t border-default ps-3 pe-2 text-12"
                  >
                    <span class="min-w-0 truncate font-mono text-highlighted" :title="file.name">
                      {{ file.name }}
                    </span>
                    <span class="grow" />
                    <span class="font-mono text-11 whitespace-nowrap text-warning">
                      {{ file.facts?.status }} {{ file.facts?.version }}
                    </span>
                    <template v-if="file.sites?.[0]">
                      <UButton
                        v-if="canMerge(file)"
                        label="Merge"
                        icon="i-lucide-git-merge"
                        color="success"
                        variant="ghost"
                        size="xs"
                        aria-label="Merge the game's changes into this copy"
                        @click="merge(file.sites[0].place)"
                      />
                      <UButton
                        label="Compare"
                        icon="i-lucide-diff"
                        variant="ghost"
                        size="xs"
                        aria-label="Compare this copy with the game's"
                        @click="compareWithGame(file.sites[0].place)"
                      />
                    </template>
                  </div>
                </ImpactBlock>
              </div>

              <div class="flex min-w-0 flex-col gap-3.5">
                <template v-for="block in tieBlocks" :key="block.key">
                  <ImpactBlock
                    v-if="block.ties.length"
                    :tone="block.tone"
                    :icon="block.icon"
                    :title="block.title"
                    :count="block.ties.length"
                    :more="
                      hasMore(`${current.impact.modId}:${block.key}`, block.ties.length)
                        ? `${describeCount(block.ties.length - ROWS_SHOWN)} more`
                        : ''
                    "
                    @more="expanded.add(`${current.impact.modId}:${block.key}`)"
                  >
                    <RouterLink
                      v-for="tie in listShown(`${current.impact.modId}:${block.key}`, block.ties)"
                      :key="`${tie.version}:${tie.article.title}`"
                      :to="{
                        name: 'patch-center',
                        params: { id: workspace.activeId() },
                        query: { patch: tie.version },
                      }"
                      class="flex h-7.5 items-center gap-2.5 border-t border-default px-3 text-12 hover:bg-accented"
                    >
                      <span class="min-w-0 truncate" :class="block.tone === 'error' ? 'text-error' : 'text-success'">
                        {{ tie.article.title }}
                      </span>
                      <span class="grow" />
                      <span class="text-11 whitespace-nowrap text-dimmed">
                        {{ plural(tie.count, "file or use", "files and uses") }}
                      </span>
                      <span class="font-mono text-11 text-dimmed">{{ tie.version }}</span>
                    </RouterLink>
                  </ImpactBlock>
                </template>
              </div>
            </div>
          </div>
        </section>
      </div>
    </template>
  </div>
</template>
