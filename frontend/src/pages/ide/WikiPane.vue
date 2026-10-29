<!--
  The Wiki pane: the pages for the active file, a search of the game's wiki, the article, and the Lexicon tab. It sits
  beside the editors or in a window of its own, and shows the page or lexicon entry the editor looks up.
-->
<script lang="ts">
/** The lexicon's classes, in the order its tabs show them. */
export const CLASSES = [
  { label: "Effects", value: "effect" },
  { label: "Triggers", value: "trigger" },
  { label: "Links", value: "link" },
  { label: "Scopes", value: "scope" },
  { label: "Functions", value: "function" },
];
</script>

<script setup lang="ts">
import { computed, shallowRef, useTemplateRef, watch } from "vue";
import { useQuery } from "@pinia/colada";
import { refDebounced, useEventBus, useLocalStorage } from "@vueuse/core";
import { Browser } from "@wailsio/runtime";
import type { DropdownMenuItem } from "@nuxt/ui";

import type { Place } from "@services/workspace/models";

import { matchQuery } from "../../lib/format";
import { lexiconQuery, stateQuery, wikiPageQuery, wikiPagesQuery, wikiSearchQuery } from "../../queries";
import { useSettingsStore } from "../../stores/settings";
import { THIS_WINDOW, useWindowsStore, type WikiLookup } from "../../stores/windows";
import { useWorkspaceStore } from "../../stores/workspace";
import ScriptSnippet from "../../ui/ScriptSnippet.vue";
import SourceLink from "../../ui/SourceLink.vue";
import WikiArticle from "../../ui/WikiArticle.vue";

const props = defineProps<{ place: Place | null }>();

/** How many pages the strip shows as tabs before the rest go in a menu. */
const PAGE_TABS = 3;
/** How many example uses a lexicon row lists. */
const EXAMPLES = 3;
const TABS = [
  { label: "Pages", value: "pages" },
  { label: "Lexicon", value: "lexicon" },
];

const settings = useSettingsStore();
const windows = useWindowsStore();
const workspace = useWorkspaceStore();
const { data: state } = useQuery(stateQuery);
const wiki = useLocalStorage("pmt.ide.wiki", false);
const view = shallowRef("pages");
const title = shallowRef("");
const lexiconClass = shallowRef("effect");
const filter = shallowRef("");

const file = shallowRef<Place | null>(null);
watch(
  () => props.place,
  (open) => {
    if (open && (!file.value || settings.isOn("ide.wikiFollow"))) file.value = open;
  },
  { immediate: true },
);
const {
  data: summaries,
  error,
  isPending,
} = useQuery(() => ({ ...wikiPagesQuery(file.value?.path ?? ""), enabled: !!file.value && workspace.isOpen() }));
const pages = computed(() => summaries.value ?? []);
watch(
  () => file.value?.key,
  () => (title.value = ""),
);
// the page on show: the one picked or linked to, else the file's first
const current = computed(() => title.value || pages.value[0]?.title || "");
const { data: article } = useQuery(() => ({
  ...wikiPageQuery(current.value),
  enabled: !!current.value && view.value === "pages",
}));
const host = computed(() => (article.value ? new URL(article.value.url).host : ""));
const more = computed<DropdownMenuItem[]>(() =>
  pages.value.map((summary) => ({
    label: summary.title,
    icon: summary.title === current.value ? "i-lucide-check" : undefined,
    onSelect: () => (title.value = summary.title),
  })),
);

/** follow shows the page a link in the article is to, when it is to one. */
function follow(event: MouseEvent): void {
  const link = event.target instanceof Element ? event.target.closest("a") : null;
  if (link?.dataset.page) title.value = link.dataset.page;
}

// a look-up from the editor shows its page, or its lexicon entry with the lexicon filtered to its name
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

// the search finds the game's wiki pages by their titles, then by their text; the page picked is shown here
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

const { data: rows } = useQuery(() => ({
  ...lexiconQuery(lexiconClass.value),
  enabled: workspace.isOpen() && view.value === "lexicon",
}));
const visible = computed(() => (rows.value ?? []).filter((row) => matchQuery(filter.value, row.name, row.blurb)));

// the letter index beside the list: each first letter with the first row it begins, and the letter at the top of view
const list = useTemplateRef("list");
const letters = computed(() =>
  visible.value.reduce<{ letter: string; index: number }[]>((found, row, index) => {
    const letter = row.name.charAt(0).toUpperCase();
    return found.at(-1)?.letter === letter ? found : [...found, { letter, index }];
  }, []),
);
const topLetter = shallowRef("");

/**
 * markLetter marks the letter of the first row in view. The rows drawn above the view to scroll smoothly are skipped.
 */
function markLetter(): void {
  const virtualizer = list.value?.virtualizer;
  const top = virtualizer?.scrollOffset ?? 0;
  const first = virtualizer?.getVirtualItems().find((item) => item.end > top)?.index ?? 0;
  topLetter.value = visible.value[first]?.name.charAt(0).toUpperCase() ?? "";
}

/** jump scrolls the list to the first row that begins with a letter. */
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
      <UTooltip v-if="THIS_WINDOW !== 'wiki'" text="Open in its own window">
        <UButton
          icon="i-lucide-app-window"
          variant="ghost"
          size="sm"
          square
          aria-label="Open in its own window"
          @click="windows.popOut({ wiki: true })"
        />
      </UTooltip>
      <UButton
        icon="i-lucide-x"
        variant="ghost"
        size="sm"
        square
        :aria-label="THIS_WINDOW === 'wiki' ? 'Back into the main window' : 'Close the Wiki pane'"
        @click="THIS_WINDOW === 'wiki' ? windows.dockBack() : (wiki = false)"
      />
    </div>
    <template v-if="view === 'pages'">
      <div class="flex h-7.5 shrink-0 items-stretch overflow-hidden border-b border-default">
        <button
          v-for="summary in pages.slice(0, PAGE_TABS)"
          :key="summary.title"
          type="button"
          class="relative flex h-7.5 shrink-0 items-center border-e border-default ps-2.5 pe-1.5 text-12 whitespace-nowrap"
          :class="
            summary.title === current
              ? 'bg-default text-highlighted before:absolute before:inset-x-0 before:bottom-0 before:h-0.5 before:bg-primary'
              : 'text-dimmed hover:text-toned'
          "
          @click="title = summary.title"
        >
          {{ summary.title }}
        </button>
        <span class="grow" />
        <UDropdownMenu v-if="pages.length > PAGE_TABS" :items="more">
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
      <!-- the field keeps no value: the page picked shows in the article below, as a link followed does -->
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
          :items="CLASSES"
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
          :virtualize="{ estimateSize: 120 }"
          class="min-h-0 flex-1"
          @scroll="markLetter"
        >
          <div class="flex flex-col gap-1.5 border-b border-default px-3.5 py-2.5">
            <div class="flex flex-wrap items-center gap-1.5">
              <span class="font-mono text-13 font-semibold break-all text-highlighted">{{ item.name }}</span>
              <UBadge v-for="scope in item.scopes ?? []" :key="scope" :label="scope" size="sm" />
              <span v-if="item.target" class="font-mono text-11 text-dimmed">→ {{ item.target }}</span>
            </div>
            <p v-if="item.blurb" class="text-12 text-toned">{{ item.blurb }}</p>
            <p v-if="item.defined" class="flex flex-wrap items-center gap-1 text-11 text-dimmed">
              Scripted, in <SourceLink :place="item.defined.place" :line="item.defined.line" />
            </p>
            <ScriptSnippet
              v-if="item.usage"
              :text="item.usage"
              language="paradox-script"
              class="rounded-md border border-default px-2.5 pb-1.5"
            />
            <div v-if="item.examples?.length" class="flex flex-col items-start gap-0.5">
              <SourceLink
                v-for="example in item.examples.slice(0, EXAMPLES)"
                :key="`${example.place.key}:${example.line}`"
                :place="example.place"
                :line="example.line"
              />
              <span v-if="item.examples.length > EXAMPLES" class="text-11 text-dimmed"
                >+{{ item.examples.length - EXAMPLES }} more</span
              >
            </div>
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
