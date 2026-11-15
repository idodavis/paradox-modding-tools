<!--
  Your mods: what the game's patches changed under each mod. Three panels say
  it for all the mods, a list ranks the mods worst first, and the mod chosen
  shows its renamed names, the game files its copies replace and the notes
  that name what it holds.
-->
<script setup lang="ts">
import { computed, shallowRef } from "vue";
import { useMutation, useQuery, useQueryCache } from "@pinia/colada";
import { groupBy, sortBy, sumBy, uniq } from "es-toolkit";

import type { Finding } from "@services/health/models";
import * as patch from "@services/patch/service";
import type { Place } from "@services/workspace/models";
import * as workspaceApi from "@services/workspace/service";
import { compareWithGame } from "../../app/commands";
import { toast } from "../../lib/feedback";
import { describeCount, list, plural } from "../../lib/format";
import { keys, notesQuery, reportQuery, stateQuery, tiesQuery, useLevels } from "../../queries";
import { useEditorStore } from "../../stores/editor";
import { useWorkspaceStore } from "../../stores/workspace";
import LevelMeter from "../../ui/LevelMeter.vue";
import OriginChip from "../../ui/OriginChip.vue";
import SourceLink from "../../ui/SourceLink.vue";
import VerdictHead from "../../ui/VerdictHead.vue";
import ImpactBlock from "./ImpactBlock.vue";

/** The splitter's two panes, the mods and the chosen mod. */
const PANES = [{ slot: "list", defaultSize: 34, minSize: 20, maxSize: 60 }, { slot: "detail" }];

const workspace = useWorkspaceStore();
const editor = useEditorStore();
const cache = useQueryCache();
const { data: state } = useQuery(stateQuery);
const { findLevel } = useLevels();
const { data: notes } = useQuery(() => ({ ...notesQuery, enabled: workspace.isOpen() }));
const { data: ties } = useQuery(() => ({ ...tiesQuery, enabled: workspace.isOpen() }));
const { data: report } = useQuery(() => ({ ...reportQuery(workspace.listModIds()), enabled: workspace.isOpen() }));

/** The game's version, where every mod's patches end. */
const end = computed(() => state.value?.install.version ?? "");
/** The releases with known changes, newest first. */
const releases = computed(() =>
  (notes.value ?? []).filter((note) => note.known && !note.beta).map((note) => note.version),
);
const chosen = shallowRef("");

/** The mods, each with what the patches changed under it. */
const cards = computed(() => {
  const findings = groupBy(report.value?.patch.findings ?? [], (finding) => finding.origin);
  const tied = groupBy(ties.value ?? [], (tie) => tie.modId);
  return workspace.listMods().map((mod) => {
    const codes = groupBy(findings[mod.id] ?? [], (finding) => finding.code);
    const renamed = codes["patch-renamed"] ?? [];
    const removed = codes["patch-removed"] ?? [];
    const files = codes["stale-copy"] ?? [];
    const breaking = (tied[mod.id] ?? []).filter((tie) => tie.breaking);
    const added = (tied[mod.id] ?? []).filter((tie) => tie.new);
    const counts = [
      { word: "renamed", count: renamed.length, color: levelColor(renamed[0]) },
      { word: "removed", count: removed.length, color: levelColor(removed[0]) },
      { word: files.length === 1 ? "file" : "files", count: files.length, color: levelColor(files[0]) },
      { word: "breaking", count: breaking.length, color: "error" },
      { word: "new", count: added.length, color: "success" },
    ].filter((row) => row.count);
    // Made for the game's version, as Go judged it, and nothing changed in it.
    const quiet = !!mod.start && mod.judged && !counts.length && !codes["supported-version"];
    return { mod, renamed, removed, files, breaking, added, counts, quiet };
  });
});
type Card = (typeof cards.value)[number];

const ranked = computed(() =>
  sortBy(cards.value, [
    (card) => -(card.renamed.length + card.removed.length + card.breaking.length),
    (card) => -card.files.length,
    (card) => -card.added.length,
  ]),
);
/**
 * The list's groups: the user's mods that have something to do, those that
 * have nothing, and the mods of others, which are shown and never patched.
 */
const groups = computed(() => {
  const own = ranked.value.filter((card) => !card.mod.external);
  return [
    { title: "", cards: own.filter((card) => !card.quiet) },
    { title: `Made for ${end.value}, nothing to do`, cards: own.filter((card) => card.quiet) },
    { title: "External mods · not yours to fix", cards: ranked.value.filter((card) => card.mod.external) },
  ].filter((group) => group.cards.length);
});
const current = computed(() => ranked.value.find((card) => card.mod.id === chosen.value) ?? groups.value[0]?.cards[0]);
/** The user's own mods, which the panels count. */
const owned = computed(() => cards.value.filter((card) => !card.mod.external));
/** The chosen mod's blocks of names and of notes that name what it holds. */
const nameBlocks = computed(() => [
  { title: "Names the game renamed", findings: current.value?.renamed ?? [] },
  { title: "Names the game removed", findings: current.value?.removed ?? [] },
]);
const tieBlocks = computed(() => [
  {
    tone: "error",
    icon: "i-lucide-triangle-alert",
    title: "Breaking changes in what it holds",
    ties: current.value?.breaking ?? [],
  },
  { tone: "success", icon: "i-lucide-star", title: "New features it can use", ties: current.value?.added ?? [] },
]);
const busiest = computed(() => Math.max(1, ...cards.value.map((card) => sumBy(card.counts, (row) => row.count))));
const panels = computed(() => {
  const names = owned.value.flatMap((card) => [...card.renamed, ...card.removed]);
  const withNames = owned.value.filter((card) => card.renamed.length + card.removed.length);
  const withFiles = owned.value.filter((card) => card.files.length);
  const mergeable = sumBy(owned.value, (card) => card.files.filter(canMerge).length);
  const breaking = sumBy(owned.value, (card) => card.breaking.length);
  const added = sumBy(owned.value, (card) => card.added.length);
  const versions = owned.value.flatMap((card) => [...card.breaking, ...card.added].map((tie) => tie.version));
  const shownNames = uniq(names.map((finding) => finding.name));
  const nameMore = shownNames.length > 2 ? [`${describeCount(shownNames.length - 2)} more`] : [];
  return [
    {
      icon: "i-lucide-tag",
      kicker: "Names the game changed",
      color: names.length ? "error" : "success",
      verdict: describeVerdict(
        [
          { count: sumBy(owned.value, (card) => card.renamed.length), word: "renamed" },
          { count: sumBy(owned.value, (card) => card.removed.length), word: "removed" },
        ],
        "Nothing renamed or removed",
      ),
      note: [
        list([...shownNames.slice(0, 2), ...nameMore]),
        list(withNames.map((card) => workspace.originName(card.mod.id))),
        list(uniq(names.map((finding) => finding.facts?.version ?? ""))),
      ]
        .filter(Boolean)
        .join(" · "),
    },
    {
      icon: "i-lucide-file",
      kicker: "Game files under your copies",
      color: withFiles.length ? levelColor(withFiles[0]?.files[0]) : "success",
      verdict: describeVerdict(
        [{ count: sumBy(withFiles, (card) => card.files.length), word: "changed" }],
        "Nothing changed",
      ),
      note: [
        ...withFiles.map((card) => `${workspace.originName(card.mod.id)} ${describeCount(card.files.length)}`),
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

/** levelColor returns the color of a finding's level, or info with none. */
function levelColor(finding: Finding | undefined): string {
  return findLevel(finding?.level)?.color ?? "info";
}

/** canMerge reports whether the game's changes to a file can be merged. */
function canMerge(file: Finding): boolean {
  return !!file.facts?.base;
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
function describeRange({ mod }: Card): string {
  return `${mod.start || (mod.judged && mod.descriptor.supportedVersion) || "no version"} → ${end.value}`;
}

/** describeEmpty says why a mod has nothing listed. */
function describeEmpty({ mod }: Card): string {
  if (mod.start) return "Nothing the patches changed is in this mod.";
  if (mod.external) return "This mod names no game version PMT can read, so PMT cannot count the patches for it.";
  if (!mod.judged) return "Pick the version this mod is made for, and PMT shows what the patches changed under it.";
  return `PMT knows no patch between ${mod.descriptor.supportedVersion} and ${end.value} yet.`;
}
</script>

<template>
  <div class="flex min-h-0 flex-col gap-3.5 px-6 py-4">
    <UEmpty v-if="!cards.length" icon="i-lucide-layers" title="This workspace has no mods" size="sm" class="m-auto" />
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

      <USplitter
        :items="PANES"
        auto-save-id="pmt.patchCenter.mods"
        class="min-h-0 flex-1 overflow-hidden rounded-lg border border-default"
      >
        <template #list>
          <section class="flex h-full w-full flex-col overflow-y-auto border-e border-default bg-muted">
            <div class="flex h-9 shrink-0 items-center gap-2 border-b border-default px-3 text-12 font-semibold">
              Mods<span class="text-11 font-normal text-dimmed">{{ cards.length }}</span>
              <span class="grow" />
              <span class="text-11 font-normal text-dimmed">worst first</span>
            </div>
            <template v-for="group in groups" :key="group.title">
              <div
                v-if="group.title"
                class="flex h-7.5 shrink-0 items-center border-b border-default bg-elevated px-3 text-11 text-dimmed"
              >
                {{ group.title }}
              </div>
              <button
                v-for="card in group.cards"
                :key="card.mod.id"
                type="button"
                class="flex shrink-0 flex-col gap-1.5 border-b border-default border-s-2 px-3 py-2.5 text-start hover:bg-accented"
                :class="[
                  card === current ? 'border-s-primary bg-(--accent-soft)' : 'border-s-transparent',
                  card.quiet && 'opacity-70',
                ]"
                @click="chosen = card.mod.id"
              >
                <span class="flex min-w-0 items-center gap-2">
                  <OriginChip :origin="card.mod.id" dot />
                  <span class="min-w-0 truncate text-13 font-semibold text-highlighted">
                    {{ workspace.originName(card.mod.id) }}
                  </span>
                  <span class="grow" />
                  <span class="font-mono text-11.5 whitespace-nowrap text-dimmed">{{ describeRange(card) }}</span>
                </span>
                <template v-if="!card.quiet">
                  <LevelMeter :segments="card.counts" :scale="busiest" />
                  <span class="flex items-center gap-2.5 text-11.5 whitespace-nowrap text-dimmed">
                    <span v-for="row in card.counts" :key="row.word">
                      <b class="font-semibold" :style="{ color: `var(--ui-${row.color})` }">
                        {{ describeCount(row.count) }}
                      </b>
                      {{ row.word }}
                    </span>
                    <span v-if="!card.counts.length" class="truncate" :title="describeEmpty(card)">
                      {{ describeEmpty(card) }}
                    </span>
                  </span>
                </template>
              </button>
            </template>
          </section>
        </template>

        <template #detail>
          <section v-if="current" class="flex h-full min-w-0 grow flex-col bg-(--bg-0)">
            <div class="flex h-13 shrink-0 items-center gap-2.5 border-b border-default bg-muted px-4">
              <OriginChip :origin="current.mod.id" dot />
              <span class="min-w-0 truncate text-16">{{ workspace.originName(current.mod.id) }}</span>
              <span class="font-mono text-12 whitespace-nowrap text-dimmed">{{ describeRange(current) }}</span>
              <span class="grow" />
              <UButton
                label="Open in the IDE"
                icon="i-lucide-code"
                variant="ghost"
                :to="{ name: 'workspace-ide', params: { id: workspace.activeId() } }"
              />
            </div>
            <!-- Someone else's mod is never patched, and one made for the -->
            <!-- game's version has nothing to pick or mark. -->
            <div
              v-if="!current.mod.external && !(current.mod.judged && current.mod.start === end)"
              class="flex h-10 shrink-0 items-center gap-2 border-b border-default bg-elevated px-4 text-12 text-dimmed"
            >
              <template v-if="!current.mod.judged">
                <span>No version given, so PMT compares from</span>
                <USelectMenu
                  :model-value="current.mod.base || undefined"
                  :items="releases"
                  :search-input="false"
                  placeholder="Pick a version"
                  size="xs"
                  class="w-32"
                  @update:model-value="setBase({ modId: current.mod.id, base: `${$event}` })"
                />
              </template>
              <span class="grow" />
              <UButton
                :label="`Mark as made for ${end}`"
                variant="subtle"
                size="sm"
                @click="markMadeFor(current.mod.id)"
              />
            </div>

            <UEmpty
              v-if="!current.counts.length"
              icon="i-lucide-circle-check"
              :title="describeEmpty(current)"
              size="sm"
              class="m-auto"
            />
            <div v-else :key="current.mod.id" class="@container min-h-0 flex-1 overflow-y-auto p-4">
              <div class="grid grid-cols-1 items-start gap-3.5 @3xl:grid-cols-[minmax(0,1.25fr)_minmax(0,1fr)]">
                <div class="flex min-w-0 flex-col gap-3.5">
                  <ImpactBlock
                    v-for="block in nameBlocks"
                    :key="block.title"
                    v-slot="{ row: finding }"
                    tone="error"
                    icon="i-lucide-tag"
                    :title="block.title"
                    :rows="block.findings"
                  >
                    <div class="flex h-7.5 items-center gap-2.5 border-t border-default px-3 text-12">
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

                  <ImpactBlock
                    v-slot="{ row: file }"
                    tone="warning"
                    icon="i-lucide-file"
                    title="Game files your copies replace"
                    :rows="current.files"
                    :aside="current.files.some(canMerge) ? `${current.files.filter(canMerge).length} can merge` : ''"
                  >
                    <div class="flex h-7.5 items-center gap-2.5 border-t border-default ps-3 pe-2 text-12">
                      <span class="min-w-0 truncate font-mono text-highlighted" :title="file.name">{{
                        file.name
                      }}</span>
                      <span class="grow" />
                      <span class="font-mono text-11 whitespace-nowrap text-warning">
                        {{ file.facts?.status }} {{ file.facts?.version }}
                      </span>
                      <template v-if="file.sites?.[0]">
                        <UButton
                          v-if="canMerge(file) && !current.mod.external"
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
                  <ImpactBlock
                    v-for="block in tieBlocks"
                    :key="block.title"
                    v-slot="{ row: tie }"
                    :tone="block.tone"
                    :icon="block.icon"
                    :title="block.title"
                    :rows="block.ties"
                  >
                    <RouterLink
                      :to="{
                        name: 'patch-center',
                        params: { id: workspace.activeId() },
                        query: { patch: tie.version, tab: 'digest', article: tie.id },
                      }"
                      class="flex h-7.5 items-center gap-2.5 border-t border-default px-3 text-12 hover:bg-accented"
                    >
                      <span class="min-w-0 truncate" :style="{ color: `var(--ui-${block.tone})` }">{{
                        tie.title
                      }}</span>
                      <span class="grow" />
                      <span class="text-11 whitespace-nowrap text-dimmed">
                        {{ plural(tie.count, "file or use", "files and uses") }}
                      </span>
                      <span class="font-mono text-11 text-dimmed">{{ tie.version }}</span>
                    </RouterLink>
                  </ImpactBlock>
                </div>
              </div>
            </div>
          </section>
        </template>
      </USplitter>
    </template>
  </div>
</template>
