<script setup lang="ts">
/**
 * The Wiki pane (the Ide board's Wiki, O-R11, O-R20): the wiki's pages for the open file as tabs, more in a menu;
 * a search that finds a page and its words; what the file is; the page's contents beside its article, whose links
 * to the file's other pages open here; and the page on the wiki with its license. While closed, a toast tells of
 * a new kind of file the wiki has pages for.
 */
import { computed, shallowRef, watch } from "vue";
import { useQuery } from "@pinia/colada";
import { useLocalStorage } from "@vueuse/core";
import { Browser } from "@wailsio/runtime";
import type { DropdownMenuItem } from "@nuxt/ui";
import type { Page } from "@services/internal/wiki/models";
import { useWikiArticle } from "../../composables/useWikiArticle";
import { useWikiPrefs } from "../../composables/useWikiPrefs";
import { guideQuery } from "../../queries";
import { useIdeStore } from "../../stores/ide";
import { useSettingsStore } from "../../stores/settings";
import { useWorkspaceStore } from "../../stores/workspace";

const { workspaceId } = defineProps<{ workspaceId: string }>();

const ide = useIdeStore();
const settings = useSettingsStore();
const store = useWorkspaceStore();
const toast = useToast();
const { toastAllowed } = useWikiPrefs();
const tocCollapsed = useLocalStorage("pmt.wiki.tocCollapsed", false);

// the file the pages are for: the open one, or while the pane does not follow, the one it had
const path = shallowRef("");
watch(
  () => ide.activePath,
  (open) => {
    if (open && (!path.value || settings.ideOn("ui.wiki.follow"))) path.value = open;
  },
  { immediate: true },
);
const { data: guide, error, isPending } = useQuery(() =>
  guideQuery({ id: workspaceId, path: path.value, enabled: ide.wikiOpen || toastAllowed() }),
);
const pages = computed(() => guide.value?.pages ?? []);

const query = shallowRef("");
const needle = computed(() => query.value.trim().toLowerCase());
const matches = (page: Page) =>
  page.title.toLowerCase().includes(needle.value) || (page.sections ?? []).some((s) => s.line.toLowerCase().includes(needle.value));
const shown = computed(() => (needle.value ? pages.value.filter(matches) : pages.value));
const title = shallowRef("");
const page = computed(() => shown.value.find((p) => p.title === title.value) ?? shown.value[0]);
// a search opens the first page it finds
watch(needle, () => (title.value = shown.value[0]?.title ?? title.value));

const TABS = 3;
const more = computed<DropdownMenuItem[]>(() =>
  shown.value.map((p) => ({ label: p.title, icon: p.title === page.value?.title ? "i-lucide-check" : undefined, onSelect: () => (title.value = p.title) })),
);

const { tocActive, jumpTo, onWikiClick } = useWikiArticle({
  html: () => page.value?.html,
  sections: () => page.value?.sections,
  pages,
  currentIndex: () => Math.max(pages.value.indexOf(page.value as Page), 0),
  query: needle,
  enabled: () => ide.wikiOpen,
  onPage(hit) {
    const dest = pages.value[hit.index];
    if (!dest) return;
    if (!shown.value.includes(dest)) query.value = "";
    title.value = dest.title;
  },
});

const host = computed(() => (page.value?.url ? new URL(page.value.url).host : ""));
const gameName = computed(() => store.game(store.activeWorkspace?.gameId ?? "")?.name ?? "game");

// while the pane is closed, a kind of file the wiki has pages for is told once
const told = new Set<string>();
watch(guide, (found) => {
  const kind = found?.database;
  if (ide.wikiOpen || !kind || told.has(kind) || !found.pages?.length || !toastAllowed()) return;
  told.add(kind);
  toast.add({
    title: `Wiki: ${found.pages[0]?.title}`,
    actions: [{ label: "Open", color: "neutral", variant: "outline", onClick: () => void (ide.wikiOpen = true) }],
  });
});
watch(() => workspaceId, () => told.clear());
</script>

<template>
  <aside class="flex min-h-0 flex-col border-s border-default bg-muted text-(length:--ide-ui-font)">
    <div class="flex h-9 shrink-0 items-center gap-1.5 border-b border-default ps-3 pe-1.5 text-12 font-semibold text-toned">
      Wiki
      <span class="grow" />
      <UButton icon="i-lucide-x" variant="ghost" size="sm" square aria-label="Close the Wiki pane" @click="ide.wikiOpen = false" />
    </div>
    <div class="flex h-7.5 shrink-0 items-stretch overflow-hidden border-b border-default">
      <button
        v-for="p in shown.slice(0, TABS)"
        :key="p.title"
        class="relative flex h-7.5 shrink-0 items-center border-e border-default ps-2.5 pe-1.5 text-12 whitespace-nowrap"
        :class="p.title === page?.title ? 'bg-default text-highlighted before:absolute before:inset-x-0 before:bottom-0 before:h-0.5 before:bg-primary' : 'text-dimmed hover:text-toned'"
        @click="title = p.title"
      >
        {{ p.title }}
      </button>
      <span class="grow" />
      <UDropdownMenu v-if="shown.length > TABS" :items="more">
        <UButton icon="i-lucide-chevron-down" variant="ghost" size="xs" square aria-label="More pages" class="me-1 self-center" />
      </UDropdownMenu>
    </div>
    <div class="flex shrink-0 flex-col gap-2 px-3 pt-2.5 pb-2">
      <UInput v-model="query" :placeholder="`Search the ${gameName} wiki`" icon="i-lucide-search" size="sm" />
      <span v-if="guide?.kind" class="text-11 text-dimmed">Suggested for {{ guide.kind }} files</span>
    </div>
    <div class="flex min-h-0 flex-1 border-t border-default">
      <WikiToc
        v-if="page?.sections?.length"
        v-model:collapsed="tocCollapsed"
        class="shrink-0 border-e border-default"
        :class="{ 'w-34': !tocCollapsed }"
        :sections="page.sections"
        :active="tocActive"
        @jump="jumpTo"
      />
      <div class="min-h-0 min-w-0 flex-1 overflow-x-hidden overflow-y-auto px-3.5 py-3 wrap-break-word">
        <p v-if="isPending && path" class="text-12 text-dimmed">Reading the wiki…</p>
        <p v-else-if="error" class="text-12 text-error">{{ error.message }}</p>
        <article v-else-if="page?.html" ref="article" class="art" @click="onWikiClick" v-html="page.html" />
        <p v-else class="text-12 text-dimmed">{{ path ? "The wiki has no page for this file." : "Open a file to see its wiki pages." }}</p>
      </div>
    </div>
    <div v-if="page" class="flex shrink-0 items-center gap-1 border-t border-default px-3 py-2 text-11 text-dimmed">
      <UButton :label="`Open on ${host}`" trailing-icon="i-lucide-external-link" variant="link" size="xs" class="p-0" @click="Browser.OpenURL(page.url)" />
      <span class="grow" />
      {{ guide?.attribution }}
    </div>
  </aside>
</template>
