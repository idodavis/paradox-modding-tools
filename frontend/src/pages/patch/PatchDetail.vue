<!--
  One version of the game: the wiki's patch notes, the Modding Co-op's digest
  with the mods each article touches, the script keys and data functions it
  changed, and the game files it changed.
-->
<script setup lang="ts">
import { computed, shallowRef, useTemplateRef, watch } from "vue";
import { useQuery } from "@pinia/colada";
import { Browser } from "@wailsio/runtime";
import { groupBy, keyBy } from "es-toolkit";
import { useRoute, useRouter } from "vue-router";

import type { Article } from "@internal/patch/models";
import { day, describeCount, MARKS, plural } from "../../lib/format";
import { classesQuery, notesQuery, patchQuery, reportQuery, stateQuery, tiesQuery, wikiPageQuery } from "../../queries";
import { useWorkspaceStore } from "../../stores/workspace";
import DocumentView from "../../ui/DocumentView.vue";
import FindBox from "../../ui/FindBox.vue";
import LexiconList from "../../ui/LexiconList.vue";
import OriginChip from "../../ui/OriginChip.vue";

const {
  version,
  find = "",
  tab = "",
  article = "",
} = defineProps<{
  version: string;
  find?: string;
  /** The tab shown. Without one, the digest, else the wiki's notes. */
  tab?: string;
  /** The id of the digest's article to open at. */
  article?: string;
}>();

/** The groups the digest's articles are drawn in. Plain notes have none. */
const GROUPS: { title: string; has: (article: Article) => boolean }[] = [
  { title: "Breaking changes", has: (article) => article.breaking },
  { title: "New features", has: (article) => article.new },
  { title: "", has: (article) => !article.breaking && !article.new },
];
/** The lexicon class of the functions. Script changes lists the others. */
const FUNCTIONS = "function";
const MODS_SHOWN = 3;

const router = useRouter();
const route = useRoute();
const workspace = useWorkspaceStore();
const { data: state } = useQuery(stateQuery);
const { data: classes } = useQuery(classesQuery);
const { data: notes } = useQuery(() => ({ ...notesQuery, enabled: workspace.isOpen() }));
const { data: patch } = useQuery(() => ({ ...patchQuery(version), enabled: workspace.isOpen() }));
const { data: allTies } = useQuery(() => ({ ...tiesQuery, enabled: workspace.isOpen() }));
const { data: report } = useQuery(() => ({ ...reportQuery(workspace.listModIds()), enabled: workspace.isOpen() }));
const note = computed(() => notes.value?.find((row) => row.version === version));
const { data: page } = useQuery(() => ({
  ...wikiPageQuery(note.value?.title ?? ""),
  enabled: workspace.isOpen() && !!note.value?.title,
}));

const articles = computed(() => patch.value?.articles ?? []);
const byId = computed(() => keyBy(articles.value, (article) => article.id));
const gameFiles = computed(() => patch.value?.gameFiles ?? []);
/** The mods this version's articles touch, by the article's id. */
const ties = computed(() =>
  groupBy(
    (allTies.value ?? []).filter((tie) => tie.version === version),
    (tie) => tie.id,
  ),
);
/** The mods whose file replaces a game file this version changed. */
const replacing = computed(() =>
  groupBy(
    (report.value?.patch.findings ?? []).filter(
      (finding) => finding.code === "stale-copy" && finding.facts?.version === version,
    ),
    (finding) => finding.name,
  ),
);
const gameName = computed(() => state.value?.game?.name ?? "");

const shown = computed(() => tab || (articles.value.length || !note.value?.title ? "digest" : "notes"));
/** One text to find, which follows the tabs. */
const term = shallowRef(find);
watch(
  () => find,
  (next) => (term.value = next),
);
const touching = shallowRef(false);
/** The article whose line of mods is shown whole. */
const opened = shallowRef("");

const scriptClasses = computed(() => (classes.value ?? []).filter((own) => own.id !== FUNCTIONS));
const dataClasses = computed(() => (classes.value ?? []).filter((own) => own.id === FUNCTIONS));
const scriptKind = shallowRef("effect");
const dataKind = shallowRef(FUNCTIONS);
/** How many of the version's changed keys are functions, and how many not. */
const changed = computed(() => {
  const functions = dataClasses.value[0]?.database;
  const changes = patch.value?.dumpChanges ?? [];
  const data = changes.filter((change) => change.database === functions).length;
  return { data, script: changes.length - data };
});
const tabs = computed(() => [
  { label: "Patch notes", value: "notes" },
  { label: "Modding digest", value: "digest" },
  { label: "Script changes", value: "script", badge: describeCount(changed.value.script) },
  { label: "Data types", value: "data", badge: describeCount(changed.value.data) },
  { label: "Game files", value: "files", badge: describeCount(gameFiles.value.length) },
]);
/** The digest as one document: each group's heading, then its articles. */
const digest = computed(() =>
  GROUPS.map(({ title, has }) => {
    const listed = articles.value.filter((article) => has(article) && (!touching.value || ties.value[article.id]));
    return (title && listed.length ? `<h1>${title}</h1>` : "") + listed.map((article) => article.html).join("");
  }).join(""),
);
const fileList = useTemplateRef("fileList");
/** The game file the find is at, by its path. */
const found = shallowRef("");

function showFile(index: number): void {
  found.value = gameFiles.value[index]?.rel ?? "";
  fileList.value?.virtualizer?.scrollToIndex(index, { align: "center" });
}

/**
 * isUntied reports whether a breaking or new article names nothing PMT can
 * tie to a mod.
 */
function isUntied(id: string): boolean {
  const article = byId.value[id];
  return !!article && (article.breaking || article.new) && !article.folders?.length && !article.keys?.length;
}
</script>

<template>
  <section class="flex min-h-0 min-w-0 grow flex-col bg-(--bg-0)">
    <div class="flex h-13 shrink-0 items-center gap-3 border-b border-default ps-8 pe-6">
      <h1 class="truncate text-16">Update {{ version }}</h1>
      <span v-if="note?.released" class="text-12 whitespace-nowrap text-dimmed">{{ note.released }}</span>
      <span v-if="patch?.version" class="min-w-0 truncate text-12 text-dimmed">
        {{ patch.local ? "Compared by PMT" : `Digest by the ${gameName} Modding Co-op, compared` }}
        with {{ patch.base || "nothing before it" }}
      </span>
      <span class="grow" />
      <span v-if="shown === 'notes' && page" class="text-12 whitespace-nowrap text-dimmed">
        Cached {{ day(page.fetchedAt) }}
      </span>
      <UButton
        v-if="page?.url"
        label="Open on the wiki"
        icon="i-lucide-external-link"
        variant="ghost"
        @click="Browser.OpenURL(page.url)"
      />
    </div>
    <div class="flex h-10 shrink-0 items-center gap-3 border-b border-default ps-6 pe-6">
      <UTabs
        :model-value="shown"
        :items="tabs"
        :content="false"
        size="sm"
        variant="link"
        @update:model-value="router.replace({ query: { ...route.query, tab: `${$event}` } })"
      />
      <span class="grow" />
      <FindBox
        v-if="shown === 'files' && gameFiles.length"
        v-model:term="term"
        :texts="gameFiles.map((file) => file.rel)"
        class="w-72"
        @hit="showFile"
        @close="((term = ''), (found = ''))"
      />
    </div>

    <template v-if="shown === 'notes'">
      <DocumentView v-if="page" v-model:find="term" :html="page.html" save-id="patch-notes" jump="Modding" />
      <UEmpty
        v-else
        icon="i-lucide-newspaper"
        :title="note?.title ? 'Reading the wiki\'s notes' : `The wiki has no notes for ${version}`"
        size="sm"
        class="m-auto"
      />
    </template>

    <UEmpty
      v-else-if="!patch?.version"
      icon="i-lucide-circle-help"
      :title="`${version}'s changes aren't known yet`"
      size="sm"
      class="m-auto"
    />

    <template v-else-if="shown === 'digest'">
      <UEmpty
        v-if="!articles.length"
        icon="i-lucide-book-open"
        :title="
          patch.local
            ? 'PMT compared this version itself and has no written digest of it'
            : 'The digest of this version has no written notes'
        "
        size="sm"
        class="m-auto"
      />
      <DocumentView v-else v-model:find="term" :html="digest" :anchor="article" save-id="digest">
        <template #rail>
          <USwitch v-model="touching" label="Only what touches my mods" size="xs" class="ps-1.5" />
        </template>
        <template #entry="{ id }">
          <OriginChip v-for="tie in ties[id]?.slice(0, MODS_SHOWN)" :key="tie.modId" :origin="tie.modId" dot />
          <span v-if="(ties[id]?.length ?? 0) > MODS_SHOWN" class="text-11 text-toned">
            +{{ (ties[id]?.length ?? 0) - MODS_SHOWN }}
          </span>
          <UTooltip v-if="isUntied(id)" text="Names no folder or definition, so PMT cannot say which mods it touches">
            <UIcon name="i-lucide-triangle-alert" class="size-3.5 shrink-0 text-warning" />
          </UTooltip>
        </template>
        <template #section="{ id }">
          <div v-if="ties[id]" class="flex flex-wrap items-center gap-1.5 pb-2 text-12 text-dimmed">
            In your mods
            <span
              v-for="tie in opened === id ? ties[id] : ties[id]?.slice(0, MODS_SHOWN)"
              :key="tie.modId"
              class="flex items-center gap-1.5"
            >
              <OriginChip :origin="tie.modId" />
              {{ plural(tie.count, "file or use", "files and uses") }}
            </span>
            <UButton
              v-if="opened !== id && (ties[id]?.length ?? 0) > MODS_SHOWN"
              :label="`+${(ties[id]?.length ?? 0) - MODS_SHOWN}`"
              variant="link"
              size="xs"
              class="p-0"
              @click="opened = id"
            />
          </div>
        </template>
      </DocumentView>
    </template>

    <LexiconList
      v-else-if="shown === 'script'"
      v-model:kind="scriptKind"
      v-model:filter="term"
      :classes="scriptClasses"
      :start="patch.base"
      :end="version"
    />

    <LexiconList
      v-else-if="shown === 'data'"
      v-model:kind="dataKind"
      v-model:filter="term"
      :classes="dataClasses"
      :start="patch.base"
      :end="version"
    />

    <UEmpty
      v-else-if="!gameFiles.length"
      icon="i-lucide-file"
      :title="
        patch.local
          ? 'PMT found no game file this version changed'
          : 'This version\'s digest doesn\'t list the game files it changed'
      "
      size="sm"
      class="m-auto"
    />
    <UScrollArea
      v-else
      ref="fileList"
      v-slot="{ item }"
      :items="gameFiles"
      :virtualize="{ estimateSize: 28, skipMeasurement: true }"
      class="min-h-0 flex-1 px-6"
    >
      <div
        class="flex h-7 items-center gap-3 rounded-sm px-2 font-mono text-12 text-toned"
        :class="item.rel === found && 'bg-(--accent-soft) ring-1 ring-primary'"
      >
        <span class="w-16 shrink-0 text-11" :class="MARKS[item.status]?.color">{{ item.status }}</span>
        <span class="min-w-0 truncate" :title="item.rel">{{ item.rel }}</span>
        <OriginChip v-for="finding in replacing[item.rel]" :key="finding.origin" :origin="finding.origin" dot />
      </div>
    </UScrollArea>
  </section>
</template>
