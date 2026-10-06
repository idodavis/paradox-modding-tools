<script setup lang="ts">
/**
 * Patch Notes (the PatchNotes board): the game's patch notes from its wiki. The versions, newest first by
 * minor version, each with its hotfixes after it, smaller; a dot where a note has a Modding section; the
 * shown note's headings; the note, with Find in these notes (Ctrl F; Enter and Shift Enter step through its
 * hits) and Open on the wiki. `?patch=` names the version shown, else the latest.
 */
import { computed, shallowRef, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useQuery } from "@pinia/colada";
import { useLocalStorage, useTimeAgo } from "@vueuse/core";
import { Browser } from "@wailsio/runtime";
import { useWikiArticle } from "../composables/useWikiArticle";
import { patchPageQuery, patchesQuery } from "../queries";
import { useWorkspaceStore } from "../stores/workspace";

defineProps<{ id: string }>();

const route = useRoute();
const router = useRouter();
const store = useWorkspaceStore();
const game = computed(() => store.originName(store.originVanilla));
const gameId = computed(() => store.activeWorkspace?.gameId ?? "");

const { data: list } = useQuery(() => patchesQuery(gameId.value));
const pages = computed(() => list.value?.pages ?? []);
const cached = useTimeAgo(() => list.value?.fetched || Date.now());
/** A version's row; a hotfix's row is the board's smaller .hot. */
type Version = { label: string; value: string; modding?: boolean; latest?: boolean; hotfix?: boolean; class?: string };
// in Go's order: newest first, a version before its hotfixes
const versions = computed(() =>
  pages.value.map((entry): Version => {
    const key = entry.version || entry.title;
    const hotfix = !!entry.parent;
    return { label: key, value: key, modding: entry.modding, latest: entry.latest, hotfix, class: hotfix ? "h-6.5 ps-5.5 text-12" : undefined };
  }),
);
// the version shown is the route's, else the latest
const selected = computed({
  get: () => {
    const want = String(route.query.patch ?? "");
    const rows = versions.value;
    return (rows.find((row) => row.value === want) ?? rows.find((row) => row.latest) ?? rows[0])?.value ?? "";
  },
  set: (patch: string | undefined) => patch && void router.replace({ query: { patch } }),
});
const { data: page } = useQuery(() => patchPageQuery({ gameId: gameId.value, version: selected.value }));

const query = shallowRef("");
const { tocActive, hits, hit, showHit, jumpTo, onWikiClick } = useWikiArticle({
  html: () => page.value?.html,
  pages: () => (page.value ? [{ title: page.value.title, url: page.value.url }] : []),
  currentIndex: 0,
  sections: () => page.value?.sections,
  query,
});
watch(selected, () => (tocActive.value = ""));

const versionsWidth = useLocalStorage("pmt.patchNotes.versionsWidth", 248);
const tocWidth = useLocalStorage("pmt.patchNotes.tocWidth", 208);
const tocCollapsed = useLocalStorage("pmt.patchNotes.tocCollapsed", false);
</script>

<template>
  <div class="flex h-full min-h-0 overflow-hidden">
    <p v-if="!pages.length" class="p-6 text-13 text-dimmed">
      No patch notes yet. PMT reads them from the {{ game }} wiki when it starts, and they show here once read.
    </p>
    <template v-else>
      <aside class="flex shrink-0 flex-col border-e border-default bg-muted" :style="{ width: `${versionsWidth}px` }">
        <div class="flex h-11 shrink-0 items-center px-3 text-12 font-semibold">
          Versions<span class="grow" /><span class="text-11 font-normal text-dimmed">{{ pages.length }}</span>
        </div>
        <UListbox
          v-model="selected"
          :items="versions"
          value-key="value"
          :filter="{ placeholder: 'Filter versions', icon: 'i-lucide-search', size: 'sm', variant: 'outline' }"
          class="min-h-0 flex-1"
          :ui="{
            root: 'gap-0 border-0',
            input: 'mx-2.5 mb-1 border-0',
            content: 'max-h-none divide-y-0 px-1.5',
            item: [
              'h-7.5 items-center rounded-md px-2.5 text-13 text-muted before:rounded-md',
              'data-[state=checked]:border-s-2 data-[state=checked]:border-primary data-[state=checked]:before:bg-(--accent-soft)',
            ],
            itemTrailingIcon: 'hidden',
          }"
        >
          <template #item-label="{ item }">
            <span v-if="item.hotfix" class="font-mono text-11 text-muted">{{ item.label }}</span>
            <span v-else class="w-13 shrink-0 font-mono text-12 font-medium text-highlighted">{{ item.label }}</span>
          </template>
          <template #item-trailing="{ item }">
            <UBadge v-if="item.latest" label="Latest" color="success" size="sm" />
            <OriginDot v-if="item.modding" color="var(--link)" small />
          </template>
        </UListbox>
        <div class="flex items-center gap-1.5 border-t border-default px-3 py-2 text-11 text-dimmed">
          <OriginDot color="var(--link)" small />Has a modding section
        </div>
      </aside>
      <ResizeGrip v-model="versionsWidth" :min="180" :max="400" :default-size="248" />
      <template v-if="page?.sections?.length">
        <WikiToc
          v-model:collapsed="tocCollapsed"
          class="shrink-0 border-e border-default"
          :style="tocCollapsed ? undefined : { width: `${tocWidth}px` }"
          :sections="page.sections"
          :active="tocActive"
          @jump="jumpTo"
        />
        <ResizeGrip v-if="!tocCollapsed" v-model="tocWidth" :min="140" :max="360" :default-size="208" />
      </template>
      <section class="flex min-w-0 grow flex-col bg-(--bg-0)">
        <div class="flex h-13 shrink-0 items-center gap-3 border-b border-default ps-8 pe-6">
          <h1 class="truncate text-16">{{ page?.title ?? selected }}</h1>
          <span class="grow" />
          <FindBox v-model="query" :hit="hit" :hits="hits" placeholder="Find in these notes" class="w-65 shrink-0" @step="(by) => showHit(hit + by)" />
          <UButton v-if="page?.url" label="Open on the wiki" icon="i-lucide-external-link" variant="ghost" @click="Browser.OpenURL(page.url)" />
        </div>
        <div class="min-h-0 flex-1 overflow-y-auto px-8 pt-2 pb-8">
          <article v-if="page?.html" ref="article" class="art" @click="onWikiClick" v-html="page.html" />
          <p v-else class="text-13 text-dimmed">No notes for this version.</p>
        </div>
      </section>
    </template>
    <StatusFacts>
      <span>Patch notes from the {{ game }} wiki</span>
      <span v-if="list?.fetched">Cached {{ cached }}</span>
    </StatusFacts>
  </div>
</template>
