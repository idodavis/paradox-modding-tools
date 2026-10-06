<script setup lang="ts">
/**
 * Guide column: search, page tabs, left Contents, one wiki article.
 */
import { computed, shallowRef, watch } from "vue";
import type { TabsItem } from "@nuxt/ui";
import { useQuery } from "@pinia/colada";
import { useLocalStorage } from "@vueuse/core";
import { Browser } from "@wailsio/runtime";
import { wikiKeys } from "../queries";
import { Guide } from "@services/wikiservice";
import type { Page } from "@services/internal/wiki/models";
import { useWorkspaceStore } from "../stores/workspace";
import { useIdeActiveFile } from "../composables/useIdeActiveFile";
import {
  guideOpen,
  ideVisible,
  setGuideOpen,
  useGuidePrefs,
} from "../composables/useGuidePrefs";
import { useWikiArticle } from "../composables/useWikiArticle";

const ws = useWorkspaceStore();
const path = useIdeActiveFile();
const { toastAllowed } = useGuidePrefs();
const toast = useToast();
const toastedKinds = new Set<string>();
const toastFetched = shallowRef(false);
const tab = shallowRef("");
const query = shallowRef("");
const tocCollapsed = useLocalStorage("pmt.guide.tocCollapsed", false);

const allPages = computed(() => guide.value?.pages ?? []);

const q = computed(() => query.value.trim().toLowerCase());

const { data: guide, error, isPending } = useQuery({
  key: () =>
    guideOpen.value
      ? wikiKeys.guide(ws.activeWorkspaceId, path.value)
      : wikiKeys.guide("toast", ws.activeWorkspaceId),
  query: () => Guide(ws.activeWorkspaceId, path.value),
  enabled: () => {
    if (!ws.activeWorkspaceId || !path.value) return false;
    if (!ideVisible.value) return false;
    if (guideOpen.value) return true;
    return toastAllowed() && !toastFetched.value;
  },
});

const pages = computed(() => {
  const all = guide.value?.pages ?? [];
  if (!q.value) return all;
  return all.filter((p) => pageMatches(p, q.value));
});

const tabItems = computed<TabsItem[]>(() =>
  pages.value.map((p) => ({
    label: p.title,
    value: p.title,
  })),
);

const activePage = computed(() =>
  pages.value.find((p) => p.title === tab.value) ?? pages.value[0],
);

const tocItems = computed(() => {
  const secs = activePage.value?.sections ?? [];
  if (!q.value) return secs;
  return secs.filter((s) => s.line.toLowerCase().includes(q.value));
});

watch(
  () => allPages.value.map((p) => p.title).join("\0"),
  (titles) => {
    if (!titles) {
      tab.value = "";
      return;
    }
    if (!allPages.value.some((p) => p.title === tab.value)) {
      tab.value = allPages.value[0]?.title ?? "";
    }
  },
);

watch(() => ws.activeWorkspaceId, () => {
  toastFetched.value = false;
  toastedKinds.clear();
});

watch(guide, (g) => {
  if (!g) return;
  if (!guideOpen.value) toastFetched.value = true;
  if (guideOpen.value || !ideVisible.value || !g.pages?.length || !toastAllowed()) {
    return;
  }
  const kind = g.database;
  if (!kind || toastedKinds.has(kind)) return;
  toastedKinds.add(kind);
  const title = g.pages[0]?.title ?? "Guide";
  toast.add({
    title: `Guide: ${title}`,
    actions: [{
      label: "Open",
      color: "neutral",
      variant: "outline",
      onClick: () => setGuideOpen(true),
    }],
  });
});

const currentIndex = computed(() =>
  Math.max(0, allPages.value.findIndex((p) => p.title === tab.value)),
);

const { tocActive, jumpTo, onWikiClick } = useWikiArticle({
  html: () => activePage.value?.html,
  sections: () => activePage.value?.sections,
  pages: allPages,
  currentIndex,
  query: q,
  enabled: guideOpen,
  onPage(hit) {
    const dest = allPages.value[hit.index];
    if (!dest) return;
    if (!pages.value.some((p) => p.title === dest.title)) query.value = "";
    tab.value = dest.title;
  },
});

// a find opens the first page that names it; the article then shows its first hit
watch(q, (needle) => {
  const dest = needle ? pages.value.find((p) => pageMatches(p, needle)) : undefined;
  if (dest) tab.value = dest.title;
});

function pageMatches(p: Page, needle: string): boolean {
  if (p.title.toLowerCase().includes(needle)) return true;
  return (p.sections ?? []).some((s) => s.line.toLowerCase().includes(needle));
}

function onTab(v: string | number): void {
  tab.value = String(v);
}
</script>

<template>
  <aside class="flex h-full min-h-0 w-full flex-col overflow-hidden bg-default">
    <div class="shrink-0 space-y-1.5 border-b border-default px-2 py-1.5">
      <div class="flex items-center gap-1">
        <div class="min-w-0 flex-1 truncate text-sm font-semibold text-default">
          Guide
        </div>
        <UTooltip text="Close">
          <UButton
            icon="i-lucide-x"
            color="neutral"
            variant="ghost"
            size="xs"
            @click="setGuideOpen(false)"
          />
        </UTooltip>
      </div>
      <UInput
        v-model="query"
        icon="i-lucide-search"
        size="sm"
        variant="soft"
        placeholder="Search the guide..."
        class="w-full"
      />
    </div>
    <div v-if="tabItems.length > 1" class="shrink-0 px-2">
      <UTabs
        :model-value="tab"
        :items="tabItems"
        :content="false"
        variant="link"
        size="sm"
        class="w-full"
        :ui="{ root: 'min-h-0 gap-0', list: 'flex-nowrap overflow-x-auto overflow-y-hidden [scrollbar-width:none] [&::-webkit-scrollbar]:hidden' }"
        @update:model-value="onTab"
      />
    </div>
    <div class="flex min-h-0 flex-1">
      <WikiToc
        v-if="tocItems.length"
        v-model:collapsed="tocCollapsed"
        class="shrink-0 border-e border-default"
        :class="{ 'w-40': !tocCollapsed }"
        :sections="tocItems"
        :active="tocActive"
        @jump="jumpTo"
      />
      <div
        class="min-h-0 min-w-0 flex-1 overflow-y-auto overflow-x-hidden px-3 py-2 wrap-break-word
          [&_details]:my-2 [&_summary]:cursor-pointer [&_summary]:text-sm [&_summary]:text-muted"
      >
        <p v-if="isPending" class="text-xs text-muted">Loading guide…</p>
        <p v-else-if="error" class="text-xs text-error">{{ error.message }}</p>
        <article
          v-else-if="activePage?.html"
          ref="article"
          class="art"
          @click="onWikiClick"
          v-html="activePage.html"
        />
        <p v-else class="text-xs text-muted">
          {{ path ? "No wiki guide for this file." : "Open a file to see its wiki guide." }}
        </p>
      </div>
    </div>
    <div class="shrink-0 border-t border-default px-2 py-1 text-xs text-muted">
      <a
        v-if="activePage?.url"
        :href="activePage.url"
        class="text-primary hover:underline"
        @click.prevent="Browser.OpenURL(activePage.url)"
      >
        {{ guide?.attribution || "Wiki" }}
      </a>
      <span v-else>{{ guide?.attribution }}</span>
    </div>
  </aside>
</template>
