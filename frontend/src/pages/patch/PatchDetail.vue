<!--
  One version of the game: the wiki's patch notes, the Modding Co-op's digest
  with the mods each article touches, the script keys and data functions it
  changed, and the game files it changed.
-->
<script setup lang="ts">
import { computed, shallowRef, useTemplateRef, watch } from "vue";
import { useQuery } from "@pinia/colada";
import { computedAsync, useEventListener } from "@vueuse/core";
import { Browser } from "@wailsio/runtime";
import { groupBy } from "es-toolkit";

import type { Class } from "@services/lsp/models";
import type { Article } from "@services/workspace/models";
import { describeCount, matchQuery, plural } from "../../lib/format";
import {
  classesQuery,
  lexiconQuery,
  loadQuery,
  notesQuery,
  patchQuery,
  stateQuery,
  wikiPageQuery,
} from "../../queries";
import { useWorkspaceStore } from "../../stores/workspace";
import LexiconList from "../../ui/LexiconList.vue";
import OriginChip from "../../ui/OriginChip.vue";
import WikiArticle from "../../ui/WikiArticle.vue";

const { version, find = "" } = defineProps<{
  version: string;
  /** Text to find in the notes shown. */
  find?: string;
}>();

/** The groups the digest's articles are drawn in. */
const GROUPS: { title: string; has: (article: Article) => boolean }[] = [
  { title: "Breaking changes", has: (article) => article.breaking },
  { title: "New features", has: (article) => article.new },
  { title: "Other notes", has: (article) => !article.breaking && !article.new },
];
/** The text color of a changed game file's status. */
const STATUS_COLORS: Record<string, string> = {
  added: "text-success",
  modified: "text-warning",
  deleted: "text-error",
};

/** The lexicon class of the functions. Script changes lists the others. */
const FUNCTIONS = "function";

const workspace = useWorkspaceStore();
const { data: state } = useQuery(stateQuery);
const { data: classes } = useQuery(classesQuery);
const { data: notes } = useQuery(() => ({ ...notesQuery, enabled: workspace.isOpen() }));
const { data: found } = useQuery(() => ({ ...patchQuery(version), enabled: workspace.isOpen() }));
const note = computed(() => notes.value?.find((row) => row.version === version));
const { data: page } = useQuery(() => ({
  ...wikiPageQuery(note.value?.title ?? ""),
  enabled: workspace.isOpen() && !!note.value?.title,
}));

const patch = computed(() => found.value?.[0]);
const articles = computed(() => patch.value?.articles ?? []);
const gameFiles = computed(() => patch.value?.gameFiles ?? []);
const ties = computed(() => groupBy(found.value?.[1] ?? [], (tie) => tie.article.title));
const gameName = computed(() => state.value?.game?.name ?? "");

const tab = shallowRef<string>();
const shown = computed(() => tab.value ?? (articles.value.length || !note.value?.title ? "digest" : "notes"));
const touching = shallowRef(false);
const term = shallowRef(find);
const fileTerm = shallowRef("");
watch(
  () => version,
  () => (tab.value = undefined),
);
watch(
  () => find,
  (next) => (term.value = next),
);

const scriptClasses = computed(() => (classes.value ?? []).filter((own) => own.id !== FUNCTIONS));
const dataClasses = computed(() => (classes.value ?? []).filter((own) => own.id === FUNCTIONS));
const scriptKind = shallowRef("effect");
const dataKind = shallowRef(FUNCTIONS);
const scriptCount = computedAsync(() => countChanged(scriptClasses.value), 0);
const dataCount = computedAsync(() => countChanged(dataClasses.value), 0);

const tabs = computed(() => [
  { label: "Patch notes", value: "notes" },
  { label: "Modding digest", value: "digest" },
  { label: "Script changes", value: "script", badge: describeCount(scriptCount.value) },
  { label: "Data types", value: "data", badge: describeCount(dataCount.value) },
  { label: "Game files", value: "files", badge: describeCount(gameFiles.value.length) },
]);
/** The articles to draw in each group, each with its place in the patch. */
const sections = computed(() =>
  GROUPS.map(({ title, has }) => ({
    title,
    rows: articles.value.flatMap((article, position) =>
      has(article) &&
      (!touching.value || ties.value[article.title]) &&
      matchQuery(term.value, article.title, article.markdown)
        ? [{ article, position }]
        : [],
    ),
  })).filter((section) => section.rows.length),
);
const files = computed(() => gameFiles.value.filter((file) => matchQuery(fileTerm.value, file.rel)));

const body = useTemplateRef("body");
/** The place in the patch of the article in view, or -1 for none. */
const active = shallowRef(-1);
useEventListener(body, "scroll", markActive, { passive: true });
watch([sections, shown], markActive, { flush: "post" });

/** countChanged counts the keys of the classes that this version changed. */
async function countChanged(kinds: Class[]): Promise<number> {
  if (!patch.value?.version || !workspace.isOpen()) return 0;

  const lists = await Promise.all(
    kinds.map((own) => loadQuery(lexiconQuery({ kind: own.id, start: patch.value?.base, end: version }))),
  );
  return lists.reduce((sum, rows) => sum + (rows ?? []).filter((row) => row.status).length, 0);
}

/**
 * markActive marks the last article that has scrolled past the top of the
 * list, or the first if none has.
 */
function markActive(): void {
  const top = (body.value?.getBoundingClientRect().top ?? 0) + 24;
  const drawn = [...(body.value?.querySelectorAll<HTMLElement>("article") ?? [])];
  const current = drawn.findLast((article) => article.getBoundingClientRect().top <= top) ?? drawn[0];
  active.value = Number(current?.dataset.position ?? -1);
}

/** scrollToArticle brings an article of the digest to the top of the list. */
function scrollToArticle(position: number): void {
  document.getElementById(`patch-article-${position}`)?.scrollIntoView({ block: "start" });
}
</script>

<template>
  <section class="flex min-h-0 min-w-0 grow flex-col bg-(--bg-0)">
    <div class="flex h-13 shrink-0 items-center gap-3 border-b border-default ps-8 pe-6">
      <h1 class="truncate text-16">Update {{ version }}</h1>
      <span v-if="patch?.version" class="min-w-0 truncate text-12 text-dimmed">
        {{ patch.local ? "Compared by PMT" : `Digest by the ${gameName} Modding Co-op, compared` }}
        with {{ patch.base || "nothing before it" }}
      </span>
      <span class="grow" />
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
        @update:model-value="tab = `${$event}`"
      />
      <span class="grow" />
      <UInput
        v-if="shown === 'digest'"
        v-model="term"
        icon="i-lucide-search"
        placeholder="Find in this digest"
        size="sm"
        class="w-56"
      />
      <UInput
        v-if="shown === 'files'"
        v-model="fileTerm"
        icon="i-lucide-search"
        placeholder="Find a file"
        size="sm"
        class="w-56"
      />
      <USwitch v-if="shown === 'digest'" v-model="touching" label="Only what touches my mods" size="sm" />
    </div>

    <template v-if="shown === 'notes'">
      <WikiArticle v-if="page" :page="page" :find="find" jump="Modding" class="min-h-0 flex-1" />
      <p v-else class="px-8 pt-4 text-13 text-dimmed">
        {{ note?.title ? "Reading the wiki's notes…" : `The wiki has no notes for ${version}.` }}
      </p>
    </template>

    <p v-else-if="!patch?.version" class="px-8 pt-4 text-13 text-dimmed">{{ version }}'s changes aren't known yet.</p>

    <template v-else-if="shown === 'digest'">
      <p v-if="!articles.length" class="px-8 pt-4 text-13 text-dimmed">
        {{
          patch.local
            ? "PMT compared this version itself and has no written digest of it."
            : "The digest of this version has no written notes."
        }}
      </p>
      <div v-else class="flex min-h-0 flex-1">
        <nav class="flex w-58 shrink-0 flex-col overflow-y-auto border-e border-default pt-3.5 ps-4 pe-2">
          <template v-for="section in sections" :key="section.title">
            <span class="px-2.5 pt-2 pb-1 text-11 font-medium text-dimmed">{{ section.title }}</span>
            <button
              v-for="{ article, position } in section.rows"
              :key="position"
              type="button"
              class="flex shrink-0 items-center gap-1.5 border-s-2 py-1.25 ps-4 pe-2.5 text-start text-12 leading-4 hover:text-highlighted"
              :class="position === active ? 'border-primary text-highlighted' : 'border-default text-dimmed'"
              @click="scrollToArticle(position)"
            >
              <span class="min-w-0 truncate">{{ article.title }}</span>
              <OriginChip v-for="tie in ties[article.title]" :key="tie.modId" :origin="tie.modId" dot />
            </button>
          </template>
        </nav>
        <div ref="body" class="min-w-0 flex-1 overflow-y-auto px-8 pb-8">
          <template v-for="section in sections" :key="section.title">
            <article
              v-for="{ article, position } in section.rows"
              :id="`patch-article-${position}`"
              :key="position"
              :data-position="position"
              class="max-w-190 pt-5"
            >
              <h2 class="text-14 font-semibold text-highlighted" :class="article.breaking && 'text-error'">
                {{ article.title }}
              </h2>
              <div v-if="ties[article.title]" class="flex flex-wrap items-center gap-1.5 py-2 text-12 text-dimmed">
                In your mods
                <span v-for="tie in ties[article.title]" :key="tie.modId" class="flex items-center gap-1.5">
                  <OriginChip :origin="tie.modId" />
                  {{ plural(tie.count, "file or use", "files and uses") }}
                </span>
              </div>
              <p
                v-else-if="(article.breaking || article.new) && !article.folders?.length && !article.keys?.length"
                class="flex items-center gap-1.5 py-2 text-12 text-dimmed"
              >
                <UIcon name="i-lucide-triangle-alert" class="size-3.5 shrink-0 text-warning" />
                Names no folder or definition, so PMT cannot say which mods it touches.
              </p>
              <UEditor :model-value="article.markdown" content-type="markdown" :editable="false" :mention="false" />
            </article>
          </template>
          <p v-if="!sections.length" class="pt-4 text-13 text-dimmed">No article matches.</p>
        </div>
      </div>
    </template>

    <LexiconList
      v-else-if="shown === 'script'"
      v-model:kind="scriptKind"
      :classes="scriptClasses"
      :start="patch.base"
      :end="version"
    />

    <LexiconList
      v-else-if="shown === 'data'"
      v-model:kind="dataKind"
      :classes="dataClasses"
      :start="patch.base"
      :end="version"
    />

    <template v-else>
      <p v-if="!gameFiles.length" class="px-8 pt-4 text-13 text-dimmed">
        {{
          patch.local
            ? "PMT found no game file this version changed."
            : "This version's digest doesn't list the game files it changed."
        }}
      </p>
      <UScrollArea
        v-else
        v-slot="{ item }"
        :items="files"
        :virtualize="{ estimateSize: 28, skipMeasurement: true }"
        class="min-h-0 flex-1 px-6"
      >
        <div class="flex h-7 items-center gap-3 px-2 font-mono text-12 text-toned">
          <span class="w-16 shrink-0 text-11" :class="STATUS_COLORS[item.status]">{{ item.status }}</span>
          <span class="min-w-0 truncate" :title="item.rel">{{ item.rel }}</span>
        </div>
      </UScrollArea>
    </template>
  </section>
</template>
