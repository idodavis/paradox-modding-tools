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

import {
  classesQuery,
  notesQuery,
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
import LexiconList from "../../ui/LexiconList.vue";
import DocumentView from "../../ui/DocumentView.vue";

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
const { data: titles, error } = useQuery(() => ({
  ...wikiPagesQuery(file.value?.path ?? ""),
  enabled: !!file.value && workspace.isOpen(),
}));
const pages = computed(() => titles.value ?? []);
const { data: readme } = useQuery(() => ({
  ...placeQuery(file.value?.readme ?? ""),
  enabled: !!file.value?.readme,
}));
watch(
  () => file.value?.key,
  () => (title.value = ""),
);
const current = computed(() => title.value || pages.value[0] || "");
const { data: article } = useQuery(() => ({
  ...wikiPageQuery(current.value),
  enabled: !!current.value && view.value === "pages",
}));
const host = computed(() => (article.value ? new URL(article.value.url).host : ""));
const morePages = computed<DropdownMenuItem[]>(() =>
  pages.value.map((page) => ({
    label: page,
    icon: page === current.value ? "i-lucide-check" : undefined,
    onSelect: () => (title.value = page),
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
const { data: notes } = useQuery(() => ({ ...notesQuery, enabled: workspace.isOpen() && view.value === "lexicon" }));
// The lexicon marks what the patches since this version changed.
const since = shallowRef<string | null>(null);
const releases = computed(() =>
  (notes.value ?? [])
    .filter((note) => note.known && !note.beta && note.version !== state.value?.install.version)
    .map((note) => note.version),
);
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
            v-for="(page, index) in pages"
            :key="page"
            type="button"
            class="relative flex h-7.5 shrink-0 items-center border-e border-default ps-2.5 pe-1.5 text-12 whitespace-nowrap"
            :class="[
              page === current
                ? 'bg-default text-highlighted before:absolute before:inset-x-0 before:bottom-0 before:h-0.5 before:bg-primary'
                : 'text-dimmed hover:text-toned',
              index >= fitting && 'invisible',
            ]"
            @click="title = page"
          >
            {{ page }}
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
          size="sm"
          class="w-full"
          @update:model-value="(next) => next && (title = next)"
        >
          <template v-if="searchError" #empty>{{ searchError.message }}</template>
        </UInputMenu>
      </div>
      <template v-if="article">
        <DocumentView :key="article.title" :html="article.html" save-id="wiki" />
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
      <p v-else-if="!titles && file" class="px-3.5 py-3 text-12 text-dimmed">Reading the wiki…</p>
      <UEmpty
        v-else
        icon="i-lucide-book-open"
        :title="file ? 'The wiki has no page for this file' : 'Open a file to see its wiki pages'"
        size="sm"
        class="m-auto"
      />
    </template>
    <template v-else>
      <LexiconList
        v-model:kind="lexiconClass"
        v-model:filter="filter"
        :classes="classes ?? []"
        :start="since ?? ''"
        :end="since ? (state?.install.version ?? '') : ''"
      >
        <span class="shrink-0 text-12 text-dimmed">Changed since</span>
        <USelectMenu
          v-model="since"
          :items="releases"
          :search-input="false"
          placeholder="No version"
          size="xs"
          clear
          class="w-32 shrink-0"
        />
      </LexiconList>
    </template>
  </aside>
</template>
