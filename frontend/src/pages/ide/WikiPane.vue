<!--
  The Wiki pane: the wiki's pages for the active file and the game's readme/info on it, a search of the game's wiki,
  the article, and the Lexicon tab.
-->
<script setup lang="ts">
import { computed, shallowRef, useTemplateRef, watch } from "vue";
import { useQuery } from "@pinia/colada";
import { refDebounced, useEventBus, useResizeObserver } from "@vueuse/core";
import { Browser } from "@wailsio/runtime";
import type { DropdownMenuItem } from "@nuxt/ui";

import type { Place } from "@services/workspace/models";

import { matchQuery } from "../../lib/format";
import {
  classesQuery,
  lexiconQuery,
  placeQuery,
  stateQuery,
  wikiPageQuery,
  wikiPagesQuery,
  wikiSearchQuery,
} from "../../queries";
import { useEditorStore } from "../../stores/editor";
import { useSettingsStore } from "../../stores/settings";
import { THIS_WINDOW, useWindowsStore, type WikiLookup } from "../../stores/windows";
import { useWorkspaceStore } from "../../stores/workspace";
import ScriptSnippet from "../../ui/ScriptSnippet.vue";
import SourceLink from "../../ui/SourceLink.vue";
import WikiArticle from "../../ui/WikiArticle.vue";

const props = defineProps<{ place: Place | null }>();

const TABS = [
  { label: "Pages", value: "pages" },
  { label: "Lexicon", value: "lexicon" },
];

const editor = useEditorStore();
const settings = useSettingsStore();
const windows = useWindowsStore();
const workspace = useWorkspaceStore();
const { data: state } = useQuery(stateQuery);
const ownWindow = THIS_WINDOW === "wiki";
const view = shallowRef("pages");
const title = shallowRef("");
const lexiconClass = shallowRef("effect");
const filter = shallowRef("");

const file = shallowRef<Place | null>(null);
watch(
  () => props.place,
  (place) => {
    if (place && (!file.value || settings.isOn("ide.wikiFollow"))) file.value = place;
  },
  { immediate: true },
);
const {
  data: summaries,
  error,
  isPending,
} = useQuery(() => ({ ...wikiPagesQuery(file.value?.path ?? ""), enabled: !!file.value && workspace.isOpen() }));
const pages = computed(() => summaries.value ?? []);
const { data: readme } = useQuery(() => ({
  ...placeQuery(file.value?.readme ?? ""),
  enabled: !!file.value?.readme,
}));
watch(
  () => file.value?.key,
  () => (title.value = ""),
);
const current = computed(() => title.value || pages.value[0]?.title || "");
const { data: article } = useQuery(() => ({
  ...wikiPageQuery(current.value),
  enabled: !!current.value && view.value === "pages",
}));
const host = computed(() => (article.value ? new URL(article.value.url).host : ""));
const morePages = computed<DropdownMenuItem[]>(() =>
  pages.value.map((summary) => ({
    label: summary.title,
    icon: summary.title === current.value ? "i-lucide-check" : undefined,
    onSelect: () => (title.value = summary.title),
  })),
);
// The page tabs that fit in the strip whole; the menu holds every page once some do not.
const strip = useTemplateRef("strip");
const fitting = shallowRef(Infinity);
useResizeObserver(strip, measureTabs);
watch(pages, measureTabs, { flush: "post" });

/** measureTabs counts the page tabs, from the first, that fit in the strip whole. */
function measureTabs(): void {
  const element = strip.value;
  if (!element) return;
  const tabs = [...element.children] as HTMLElement[];
  const overflow = tabs.findIndex((tab) => tab.offsetLeft + tab.offsetWidth > element.clientWidth);
  fitting.value = overflow < 0 ? tabs.length : overflow;
}

/** follow shows the wiki page a clicked link in the article points to, if it points to one. */
function follow(event: MouseEvent): void {
  const link = event.target instanceof Element ? event.target.closest("a") : null;
  if (link?.dataset.page) title.value = link.dataset.page;
}

// A look-up from the editor shows its page, or its lexicon entry.
useEventBus<WikiLookup>("pmt.wiki").on((lookup) => {
  if ("title" in lookup) {
    view.value = "pages";
    title.value = lookup.title;
    return;
  }
  view.value = "lexicon";
  lexiconClass.value = lookup.class;
  filter.value = lookup.name;
});

const term = shallowRef("");
const search = refDebounced(term, 200);
const {
  data: hits,
  error: searchError,
  isLoading,
} = useQuery(() => ({
  ...wikiSearchQuery(search.value),
  enabled: workspace.isOpen() && !!search.value.trim(),
  placeholderData: (previous) => (search.value ? previous : undefined),
}));

const { data: classes } = useQuery(classesQuery);
const { data: rows } = useQuery(() => ({
  ...lexiconQuery(lexiconClass.value),
  enabled: workspace.isOpen() && view.value === "lexicon",
}));
const visible = computed(() => (rows.value ?? []).filter((row) => matchQuery(filter.value, row.name, row.blurb)));

// The letter index beside the list: each first letter with the first row it begins.
const list = useTemplateRef("list");
const letters = computed(() =>
  visible.value.reduce<{ letter: string; index: number }[]>((starts, row, index) => {
    const letter = initial(row.name);
    return starts.at(-1)?.letter === letter ? starts : [...starts, { letter, index }];
  }, []),
);
const topLetter = shallowRef("");

/** initial returns the given text's first letter in upper case. */
function initial(text: string): string {
  return text.charAt(0).toUpperCase();
}

/** markLetter marks the first letter of the first row in view. */
function markLetter(): void {
  const virtualizer = list.value?.virtualizer;
  const top = virtualizer?.scrollOffset ?? 0;
  const first = virtualizer?.getVirtualItems().find((item) => item.end > top)?.index ?? 0;
  topLetter.value = initial(visible.value[first]?.name ?? "");
}

/** jump scrolls the lexicon list to the row at the given index. */
function jump(index: number): void {
  list.value?.virtualizer?.scrollToIndex(index, { align: "start" });
}
</script>

<template>
  <aside class="flex min-h-0 flex-col border-s border-default bg-muted text-(length:--ide-ui-font)">
    <div class="flex h-9 shrink-0 items-center gap-1.5 border-b border-default ps-1 pe-1.5">
      <UTabs
        :model-value="view"
        :items="TABS"
        :content="false"
        variant="link"
        size="sm"
        @update:model-value="view = String($event)"
      />
      <span class="grow" />
      <UTooltip v-if="!ownWindow" text="Open in its own window">
        <UButton
          icon="i-lucide-app-window"
          variant="ghost"
          size="sm"
          square
          aria-label="Open in its own window"
          @click="windows.popOut('wiki')"
        />
      </UTooltip>
      <UButton
        icon="i-lucide-x"
        variant="ghost"
        size="sm"
        square
        :aria-label="ownWindow ? 'Back into the main window' : 'Close the Wiki pane'"
        @click="ownWindow ? windows.dockBack() : (editor.wiki = false)"
      />
    </div>
    <template v-if="view === 'pages'">
      <div class="flex h-7.5 shrink-0 items-stretch border-b border-default">
        <div ref="strip" class="relative flex min-w-0 flex-1 items-stretch overflow-hidden">
          <button
            v-for="(summary, index) in pages"
            :key="summary.title"
            type="button"
            class="relative flex h-7.5 shrink-0 items-center border-e border-default ps-2.5 pe-1.5 text-12 whitespace-nowrap"
            :class="[
              summary.title === current
                ? 'bg-default text-highlighted before:absolute before:inset-x-0 before:bottom-0 before:h-0.5 before:bg-primary'
                : 'text-dimmed hover:text-toned',
              index >= fitting && 'invisible',
            ]"
            @click="title = summary.title"
          >
            {{ summary.title }}
          </button>
        </div>
        <UTooltip v-if="readme" :text="`Open the game's readme/info, ${readme.name}`">
          <UButton
            label="From the game"
            icon="i-lucide-book-open"
            color="neutral"
            variant="ghost"
            size="xs"
            class="me-1 shrink-0 self-center"
            @click="editor.show(readme)"
          />
        </UTooltip>
        <UDropdownMenu v-if="fitting < pages.length" :items="morePages">
          <UButton
            icon="i-lucide-chevron-down"
            variant="ghost"
            size="xs"
            square
            aria-label="More pages"
            class="me-1 self-center"
          />
        </UDropdownMenu>
      </div>
      <!-- The field keeps no value; the page picked shows in the article below. -->
      <div class="shrink-0 border-b border-default px-3 pt-2.5 pb-2">
        <UInputMenu
          v-model:search-term="term"
          model-value=""
          :items="hits ?? []"
          ignore-filter
          :loading="isLoading"
          :placeholder="`Search the ${state?.game?.name ?? 'game'} wiki`"
          icon="i-lucide-search"
          value-key="title"
          label-key="title"
          size="sm"
          class="w-full"
          @update:model-value="(next) => next && (title = next)"
        >
          <template v-if="searchError" #empty>{{ searchError.message }}</template>
        </UInputMenu>
      </div>
      <template v-if="article">
        <div class="flex min-h-0 flex-1 flex-col" @click="follow">
          <WikiArticle :key="article.title" :page="article" />
        </div>
        <div class="flex shrink-0 items-center gap-1 border-t border-default px-3 py-2 text-11 text-dimmed">
          <UButton
            :label="`Open on ${host}`"
            trailing-icon="i-lucide-external-link"
            variant="link"
            size="xs"
            class="p-0"
            @click="Browser.OpenURL(article.url)"
          />
          <span class="grow" />
          Text from the wiki, CC-BY-SA 3.0
        </div>
      </template>
      <p v-else-if="error" class="px-3.5 py-3 text-12 text-error">{{ error.message }}</p>
      <p v-else-if="isPending && file" class="px-3.5 py-3 text-12 text-dimmed">Reading the wiki…</p>
      <p v-else class="px-3.5 py-3 text-12 text-dimmed">
        {{ file ? "The wiki has no page for this file." : "Open a file to see its wiki pages." }}
      </p>
    </template>
    <template v-else>
      <div class="flex shrink-0 flex-wrap items-center gap-2 border-b border-default px-3 py-2">
        <UTabs
          :model-value="lexiconClass"
          :items="classes ?? []"
          value-key="id"
          :content="false"
          size="xs"
          @update:model-value="lexiconClass = String($event)"
        />
        <UInput
          v-model="filter"
          placeholder="Filter the lexicon"
          size="xs"
          icon="i-lucide-filter"
          class="ms-auto w-48"
        />
      </div>
      <div v-if="visible.length" class="flex min-h-0 flex-1">
        <UScrollArea
          ref="list"
          v-slot="{ item }"
          :items="visible"
          :virtualize="{ estimateSize: (index) => (visible[index]?.blurb || visible[index]?.usage ? 140 : 52) }"
          class="min-h-0 flex-1"
          @scroll="markLetter"
        >
          <div
            class="flex flex-col gap-1.5 border-b border-default px-3.5"
            :class="item.blurb || item.usage ? 'py-2.5' : 'py-1.5'"
          >
            <div class="flex flex-wrap items-center gap-x-2 gap-y-1">
              <span class="font-mono text-13 font-semibold break-all text-highlighted">{{ item.name }}</span>
              <UBadge v-for="scope in item.scopes ?? []" :key="scope" :label="scope" size="sm" />
              <span v-if="item.target" class="font-mono text-11 text-dimmed">→ {{ item.target }}</span>
            </div>
            <UTooltip v-if="item.defined" :text="item.defined.place.rel" class="self-start">
              <SourceLink :place="item.defined.place" :line="item.defined.line" short class="text-11 text-dimmed" />
            </UTooltip>
            <p v-if="item.blurb" class="text-12 text-toned">{{ item.blurb }}</p>
            <ScriptSnippet
              v-if="item.usage"
              :text="item.usage"
              language="paradox-script"
              class="rounded-md border border-default px-2.5 pb-1.5"
            />
          </div>
        </UScrollArea>
        <nav class="flex w-6 shrink-0 flex-col items-center overflow-y-auto border-s border-default py-1 text-10">
          <button
            v-for="{ letter, index } in letters"
            :key="letter"
            type="button"
            class="w-full shrink-0 rounded-sm py-0.5 text-center font-medium"
            :class="letter === topLetter ? 'bg-(--accent-soft) text-highlighted' : 'text-dimmed hover:text-toned'"
            :aria-label="`Rows beginning with ${letter}`"
            @click="jump(index)"
          >
            {{ letter }}
          </button>
        </nav>
      </div>
      <p v-else class="px-3.5 py-3 text-12 text-dimmed">
        {{ rows ? "Nothing in the lexicon matches." : "Reading the game's dumps…" }}
      </p>
    </template>
  </aside>
</template>
