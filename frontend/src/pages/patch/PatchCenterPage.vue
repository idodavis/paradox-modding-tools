<!--
  The Patch Center: Your mods, which says what the game's patches changed
  under each mod, and the game's versions, each with its patch notes, its
  digest and the files it changed. A line above them offers to start the game
  where its documentation is not of the installed version.
-->
<script setup lang="ts">
import { computed, watch } from "vue";
import { useQuery } from "@pinia/colada";
import { useRouter } from "vue-router";

import type { Note } from "@services/patch/models";
import { findCommand } from "../../app/commands";
import { useShown } from "../../app/pages";
import { codesQuery, notesQuery, reportQuery, stateQuery } from "../../queries";
import { useSeenPatch } from "../../stores/patchSeen";
import { makeChip, useChip } from "../../stores/toolView";
import { useWorkspaceStore } from "../../stores/workspace";
import FactText from "../../ui/FactText.vue";
import LevelCounts from "../../ui/LevelCounts.vue";
import OriginChip from "../../ui/OriginChip.vue";
import StatusFacts from "../../ui/StatusFacts.vue";
import PatchDetail from "./PatchDetail.vue";
import YourMods from "./YourMods.vue";

const {
  id,
  patch = "",
  find = "",
  tab = "",
  article = "",
} = defineProps<{
  id: string;
  /** The version shown, as the route names it. Without one, Your mods. */
  patch?: string;
  find?: string;
  /** The version's tab and the digest's article to open at. */
  tab?: string;
  article?: string;
}>();

/** A Row is a line of the version list: a version or a release's heading. */
type Row = Note & { label: string; type?: "label" };

const LAUNCH = findCommand("game.launch");
const GUIDE = findCommand("docs.guide");
const MODS_SHOWN = 3;
const PANES = [{ slot: "versions", defaultSize: 18, minSize: 14, maxSize: 30 }, { slot: "shown" }];

const router = useRouter();
const workspace = useWorkspaceStore();
const { data: state } = useQuery(stateQuery);
const { data: notes } = useQuery(() => ({ ...notesQuery, enabled: workspace.isOpen() }));
const { data: codes } = useQuery(codesQuery);
const { data: report } = useQuery(() => ({
  ...reportQuery(workspace.listModIds()),
  enabled: workspace.isOpen(),
}));

/** The list's groups: each release's versions under a heading for it. */
const groups = computed(() =>
  (notes.value ?? []).reduce<Row[][]>((all, note) => {
    const row = { ...note, label: note.version };
    const last = all.at(-1);
    if (last?.[0]?.release === note.release) last.push(row);
    else all.push([{ ...row, type: "label", label: note.beta ? `${note.release}, open beta` : note.release }, row]);

    return all;
  }, []),
);
const latest = computed(() => notes.value?.find((note) => !note.beta)?.version);
/**
 * The newest version whose changes PMT has, where it has none for the
 * version the game is on. Your mods then counts no further than it.
 */
const knownUntil = computed(() => {
  const known = (notes.value ?? []).filter((note) => note.known);
  return known.some((note) => note.installed) ? undefined : known.find((note) => !note.beta)?.version;
});
const gameName = computed(() => state.value?.game?.name ?? "");
/** Where the page's notes come from: the wiki, and the Co-op's digests. */
const sources = computed(() => {
  const digests = state.value?.game?.digests;
  return [
    { label: "Patch notes from the", value: `${gameName.value} wiki` },
    ...(digests
      ? [{ label: "Digests from the", value: `${gameName.value} Modding Co-op`, url: `https://github.com/${digests}` }]
      : []),
  ];
});
/** Health's finding that the game's documentation is stale, and its advice. */
const staleDumps = computed(() => report.value?.patch.findings?.find((finding) => finding.code === "stale-dumps"));
const advice = computed(() => codes.value?.find((code) => code.id === "stale-dumps")?.advice);

// The top bar's dot for a new version goes out once the page is shown.
const { newest, shown: seen } = useSeenPatch();
const visible = useShown();
watch(
  [visible, newest],
  ([isShown, version]) => {
    if (isShown && version) seen.value = version;
  },
  { immediate: true },
);

useChip(() => (patch ? makeChip(patch, `The ${gameName.value} patch notes of ${patch}`) : null));

/** show shows a version, or Your mods for none, by writing it to the route. */
function show(version: string): void {
  void router.replace({ query: { patch: version || undefined } });
}
</script>

<template>
  <div class="flex h-full min-h-0 overflow-hidden">
    <USplitter :items="PANES" auto-save-id="pmt.patchCenter" class="min-w-0 flex-1">
      <template #versions>
        <aside class="flex min-w-0 flex-1 flex-col border-e border-default bg-muted">
          <button
            type="button"
            class="mx-1.5 mt-2 mb-1 flex h-8 shrink-0 items-center gap-2 rounded-md px-2.5 text-13 font-medium whitespace-nowrap text-toned"
            :class="!patch && 'border-s-2 border-primary bg-(--accent-soft) ps-2 text-highlighted'"
            @click="show('')"
          >
            <UIcon name="i-lucide-layers" class="size-3.5" />
            Your mods
            <span class="grow" />
            <LevelCounts v-if="report" :levels="report.ownPatch.counts" />
          </button>
          <div class="flex h-9 shrink-0 items-center border-t border-default px-4 text-12 font-semibold">
            Versions<span class="grow" /><span class="text-11 font-normal text-dimmed">{{ notes?.length ?? 0 }}</span>
          </div>
          <UEmpty
            v-if="!notes?.length"
            icon="i-lucide-newspaper"
            title="No patch notes yet"
            :description="`PMT reads them from the ${gameName} wiki when it starts. The status bar shows how far it is.`"
            size="sm"
          />
          <UListbox
            v-else
            :model-value="patch || undefined"
            :items="groups"
            value-key="version"
            :filter="{ placeholder: 'Filter versions', icon: 'i-lucide-search', size: 'sm', variant: 'outline' }"
            class="min-h-0 flex-1"
            :ui="{
              root: 'gap-0 rounded-none ring-0',
              input: 'mx-1.5 mb-1 border-0',
              content: 'max-h-none divide-y-0 px-1.5',
              label: 'px-2.5 pt-3 pb-1 text-11 font-medium text-dimmed',
              item: [
                'h-7.5 items-center rounded-md px-2.5 text-13 text-muted before:rounded-md',
                'data-[state=checked]:border-s-2 data-[state=checked]:border-primary data-[state=checked]:ps-2 data-[state=checked]:before:bg-(--accent-soft)',
              ],
              itemTrailingIcon: 'hidden',
            }"
            @update:model-value="(version) => version && show(`${version}`)"
          >
            <template #item-label="{ item }">
              <span class="font-mono text-12" :class="item.beta ? 'text-toned' : 'font-medium text-highlighted'">
                {{ item.version }}
              </span>
            </template>
            <template #item-trailing="{ item }">
              <UBadge v-if="item.version === latest" label="Latest" color="success" size="sm" />
              <OriginChip v-for="mod in item.mods?.slice(0, MODS_SHOWN)" :key="mod" :origin="mod" dot />
              <span v-if="(item.mods?.length ?? 0) > MODS_SHOWN" class="text-11 text-toned">
                +{{ (item.mods?.length ?? 0) - MODS_SHOWN }}
              </span>
              <span
                v-if="item.modding"
                class="size-2 shrink-0 rounded-full border-2 border-(--link)"
                title="Has a modding section"
              />
              <span
                v-if="item.known"
                class="size-2 shrink-0 rounded-xs bg-(--purple)"
                title="Its code changes are known"
              />
            </template>
          </UListbox>
          <div class="flex flex-col gap-0.5 border-t border-default px-4 py-2 text-11 text-dimmed">
            <span class="flex items-center gap-1.5">
              <span class="size-2 shrink-0 rounded-full border-2 border-(--link)" />The wiki's notes have a modding
              section
            </span>
            <span class="flex items-center gap-1.5">
              <span class="size-2 shrink-0 rounded-xs bg-(--purple)" />Its code changes are known
            </span>
            <span class="flex items-center gap-1.5">
              <span class="flex shrink-0 gap-0.5">
                <span class="size-1.5 rounded-full bg-(--o1)" /><span class="size-1.5 rounded-full bg-(--o2)" /><span
                  class="size-1.5 rounded-full bg-(--o3)"
                />
              </span>
              Mods made for this version, each in its color
            </span>
          </div>
        </aside>
      </template>
      <template #shown>
        <div class="flex min-h-0 min-w-0 grow flex-col">
          <div
            v-if="staleDumps"
            class="flex shrink-0 flex-wrap items-center gap-x-3 gap-y-1.5 border-b border-default bg-muted px-6 py-2 text-12"
          >
            <UIcon name="i-lucide-triangle-alert" class="size-3.5 shrink-0 text-warning" />
            <p class="min-w-0 text-toned"><FactText :parts="staleDumps.message" /> {{ advice }}</p>
            <span class="grow" />
            <UButton
              label="Start the game to record its commands"
              icon="i-lucide-play"
              size="sm"
              @click="LAUNCH?.onSelect?.($event)"
            />
            <UButton label="Show me how" variant="ghost" size="sm" @click="GUIDE?.onSelect?.($event)" />
          </div>
          <PatchDetail v-if="patch" :version="patch" :find="find" :tab="tab" :article="article" />
          <section v-else class="flex min-h-0 min-w-0 grow flex-col bg-(--bg-0)">
            <div class="flex h-13 shrink-0 items-center gap-3 border-b border-default px-6">
              <h1 class="text-16">Your mods</h1>
              <span class="min-w-0 truncate text-12 text-dimmed">
                What changed between the version each mod is made for and the game's
                {{ state?.install.version }}
              </span>
              <span class="grow" />
              <UButton
                label="Open in Health"
                icon="i-lucide-activity"
                variant="ghost"
                :to="{ name: 'health', params: { id }, query: { view: 'patch' } }"
              />
            </div>
            <p
              v-if="knownUntil"
              class="flex shrink-0 items-center gap-3 border-b border-default bg-muted px-6 py-2 text-12 text-toned"
            >
              <UIcon name="i-lucide-triangle-alert" class="size-3.5 shrink-0 text-warning" />
              PMT does not know what {{ state?.install.version }} changed. The newest changes it knows are
              {{ knownUntil }}'s, and nothing after that is counted here.
            </p>
            <YourMods class="min-h-0 flex-1" />
          </section>
        </div>
      </template>
    </USplitter>
    <StatusFacts :facts="sources" />
  </div>
</template>
