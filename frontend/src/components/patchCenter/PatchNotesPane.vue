<script setup lang="ts">
/**
 * Wiki patch-notes reader: versions, Contents, All/Modding article.
 */
import { computed, nextTick, shallowRef, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useQuery } from "@pinia/colada";
import { PatchPage, Patches } from "@services/wikiservice";
import type { PatchEntry, Section } from "@services/internal/wiki/models";
import { useWorkspaceStore } from "../../stores/workspace";
import { useWikiArticle } from "../../composables/useWikiArticle";
import WikiToc from "../WikiToc.vue";
import { firstSectionHit } from "../../wikiArticle";

type BodyMode = "all" | "modding";

const route = useRoute();
const router = useRouter();
const ws = useWorkspaceStore();
const filter = shallowRef("");
const query = shallowRef("");

const gameId = computed(() => ws.activeWorkspace?.gameId ?? "");

const { data: list } = useQuery({
  key: () => ["wiki-patches", gameId.value],
  query: () => Patches(gameId.value),
  enabled: () => !!gameId.value,
});

const pages = computed(() => list.value?.pages ?? []);

const selected = computed(() => {
  const q = String(route.query.patch ?? "");
  if (q && pages.value.some((p) => p.version === q || p.title === q)) {
    return q;
  }
  return pages.value[0]?.version || pages.value[0]?.title || "";
});

const { data: page } = useQuery({
  key: () => ["wiki-patch-page", gameId.value, selected.value],
  query: () => PatchPage(gameId.value, selected.value),
  enabled: () => !!gameId.value && !!selected.value,
});

const mode = shallowRef<BodyMode>("modding");

watch(page, (p) => {
  mode.value = p?.hasModding ? "modding" : "all";
}, { immediate: true });

const html = computed(() => {
  switch (mode.value) {
    case "modding":
      return page.value?.moddingHtml || page.value?.html || "";
    case "all":
      return page.value?.html || "";
    default: {
      const _x: never = mode.value;
      return _x;
    }
  }
});

const tocSections = computed<Section[]>(() => {
  switch (mode.value) {
    case "modding":
      return page.value?.moddingSections ?? [];
    case "all":
      return page.value?.sections ?? [];
    default: {
      const _x: never = mode.value;
      return _x;
    }
  }
});

/** Roots plus nested hotfix children, newest first as the sidecar sent them. */
const tree = computed(() => {
  const byVer = new Map<string, PatchEntry>();
  for (const p of pages.value) {
    if (p.version) byVer.set(p.version, p);
  }
  const kids = new Map<string, PatchEntry[]>();
  const roots: PatchEntry[] = [];
  for (const p of pages.value) {
    if (p.parent && byVer.has(p.parent)) {
      const arr = kids.get(p.parent) ?? [];
      arr.push(p);
      kids.set(p.parent, arr);
    } else {
      roots.push(p);
    }
  }
  return roots.map((entry) => ({
    entry,
    children: kids.get(entry.version) ?? [],
  }));
});

const needle = computed(() => filter.value.trim().toLowerCase());

const filteredTree = computed(() => {
  if (!needle.value) return tree.value;
  return tree.value
    .map((node) => ({
      entry: node.entry,
      children: node.children.filter((c) => entryMatch(c, needle.value)),
    }))
    .filter((node) =>
      entryMatch(node.entry, needle.value) || node.children.length > 0,
    );
});

function entryMatch(p: PatchEntry, n: string): boolean {
  return (p.version || "").toLowerCase().includes(n)
    || p.title.toLowerCase().includes(n);
}

const wikiPages = computed(() =>
  page.value ? [{ title: page.value.title, url: page.value.url }] : [],
);

const { articleEl, tocActive, jumpTo, onWikiClick } = useWikiArticle({
  html,
  pages: wikiPages,
  currentIndex: 0,
  query,
});

watch(
  () => query.value.trim(),
  async (needle) => {
    if (!needle) return;
    await nextTick();
    const hit = firstSectionHit(tocSections.value, needle);
    if (hit) {
      jumpTo(hit);
      return;
    }
    articleEl.value?.querySelector("mark")?.scrollIntoView({ block: "nearest" });
  },
);

/** Deep-link this version in ?patch=. */
function selectPatch(p: PatchEntry): void {
  const patch = p.version || p.title;
  tocActive.value = "";
  void router.replace({
    query: { ...route.query, tab: "notes", patch },
  });
}

function isActive(p: PatchEntry): boolean {
  const key = p.version || p.title;
  return key === selected.value || p.title === selected.value;
}

function isLatest(p: PatchEntry): boolean {
  return tree.value[0]?.entry.title === p.title;
}

function setMode(next: BodyMode): void {
  mode.value = next;
}

function openUrl(url: string): void {
  window.open(url, "_blank");
}
</script>

<template>
  <div class="flex min-h-0 flex-1 overflow-hidden">
    <p v-if="!pages.length" class="p-3 text-sm text-muted">
      No patch notes cached. Rescan the install to fetch wiki pages.
    </p>
    <template v-else>
      <nav class="flex w-56 shrink-0 flex-col overflow-hidden border-e border-default bg-muted">
        <div class="shrink-0 space-y-1.5 px-2 py-2">
          <div class="text-xs font-semibold uppercase tracking-wide text-muted">
            Versions
          </div>
          <UInput
            v-model="filter"
            icon="i-lucide-search"
            size="sm"
            variant="soft"
            placeholder="Filter versions..."
          />
        </div>
        <div class="min-h-0 flex-1 overflow-auto py-1">
          <template v-for="node in filteredTree" :key="node.entry.title">
            <UButton
              :label="node.entry.version || node.entry.title"
              :color="isActive(node.entry) ? 'primary' : 'neutral'"
              :variant="isActive(node.entry) ? 'soft' : 'ghost'"
              size="sm"
              block
              class="justify-start truncate px-2"
              @click="selectPatch(node.entry)"
            >
              <template #trailing>
                <UBadge
                  v-if="isLatest(node.entry)"
                  label="Latest"
                  color="success"
                  variant="subtle"
                  size="xs"
                />
              </template>
            </UButton>
            <UButton
              v-for="child in node.children"
              :key="child.title"
              :label="child.version || child.title"
              :color="isActive(child) ? 'primary' : 'neutral'"
              :variant="isActive(child) ? 'soft' : 'ghost'"
              size="sm"
              block
              class="ms-3 justify-start truncate border-s border-default ps-3"
              @click="selectPatch(child)"
            />
          </template>
        </div>
      </nav>
      <WikiToc
        v-if="tocSections.length"
        :sections="tocSections"
        :active="tocActive"
        @jump="jumpTo"
      />
      <div class="flex min-h-0 min-w-0 flex-1 flex-col">
        <div class="flex shrink-0 items-center gap-2 border-b border-default px-4 py-2">
          <h2 class="min-w-0 flex-1 truncate text-2xl font-semibold tracking-tight">
            {{ page?.title || selected }}
          </h2>
          <UInput
            v-model="query"
            icon="i-lucide-search"
            size="sm"
            variant="soft"
            placeholder="Search notes..."
            class="w-48 shrink-0"
          />
          <UButton
            label="All"
            size="sm"
            :color="mode === 'all' ? 'primary' : 'neutral'"
            :variant="mode === 'all' ? 'soft' : 'ghost'"
            @click="setMode('all')"
          />
          <UButton
            label="Modding"
            size="sm"
            :disabled="!page?.hasModding"
            :color="mode === 'modding' ? 'primary' : 'neutral'"
            :variant="mode === 'modding' ? 'soft' : 'ghost'"
            @click="setMode('modding')"
          />
        </div>
        <div class="min-h-0 flex-1 overflow-y-auto overflow-x-hidden px-6 py-4 wrap-break-word">
          <div
            v-if="html"
            ref="article"
            @click="onWikiClick"
            v-html="html"
          />
          <p v-else class="text-sm text-muted">No notes for this version.</p>
        </div>
        <div class="shrink-0 border-t border-default px-4 py-1 text-xs text-muted">
          <a
            v-if="page?.url"
            :href="page.url"
            class="text-primary hover:underline"
            @click.prevent="openUrl(page.url)"
          >
            Open on wiki
          </a>
        </div>
      </div>
    </template>
  </div>
</template>
