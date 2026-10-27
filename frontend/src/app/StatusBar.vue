<!--
  The status bar: the install and its scan, the page's own facts, the wiki download, the windows, Help and the
  version with its update check.
-->
<script setup lang="ts">
import type { ToastProps } from "@nuxt/ui";
import { useMutation, useQuery } from "@pinia/colada";
import { Clipboard, Events } from "@wailsio/runtime";
import { computed, onScopeDispose, shallowRef, watch } from "vue";
import { useRoute } from "vue-router";

import * as app from "@services/app/service";
import { day, installLine } from "../lib/format";
import { crashedQuery, releasesQuery, stateQuery, updateQuery, versionQuery, windowsQuery } from "../queries";
import { describeWindow, THIS_WINDOW, useWindowsStore } from "../stores/windows";
import { PAGES } from "./pages";

const route = useRoute();
const toast = useToast();
const windows = useWindowsStore();

const page = computed(() => (route.name ? PAGES[route.name] : undefined));
const { data: state } = useQuery(stateQuery);
const { data: version } = useQuery(versionQuery);
const { data: update, refetch: checkUpdate, isLoading: checking } = useQuery({ ...updateQuery, staleTime: 3_600_000 });
const { data: releases, refetch: refetchReleases } = useQuery({ ...releasesQuery, enabled: false });
const { data: crashed } = useQuery(crashedQuery);
const { data: names } = useQuery(windowsQuery);
const { mutate: applyUpdate } = useMutation({ mutation: () => app.ApplyUpdate() });
const { mutate: copyDiagnostics } = useMutation({
  mutation: async () => {
    await Clipboard.SetText(await app.BuildDiagnostics());
    toast.add({
      title: "Diagnostics copied",
      description: "Paste them into a bug report on GitHub.",
      color: "success",
    });
  },
});

const wiki = shallowRef<{ done: number; total: number } | null>(null);
onScopeDispose(Events.On("wiki:progress", ({ data }) => (wiki.value = data)));
onScopeDispose(
  Events.On("wiki:updated", ({ data }) => {
    wiki.value = null;
    if (data && THIS_WINDOW === "main") toast.add({ title: "Wiki fetch failed", description: data, color: "error" });
  }),
);

// the install's scan while a workspace opens, as the last stage it reported
const scan = shallowRef<{ stage: string; percent: number } | null>(null);
onScopeDispose(Events.On("install:scanning", ({ data }) => (scan.value = data)));
for (const event of ["install:scanned", "workspace:opened", "workspace:failed"] as const) {
  onScopeDispose(Events.On(event, () => (scan.value = null)));
}

// the other windows, each brought to the front from the menu of the main window
const others = computed(() =>
  (names.value ?? [])
    .filter((name) => name !== THIS_WINDOW)
    .map((name) => ({ label: describeWindow(name), onSelect: () => windows.showWindow(name) })),
);

// what the main window toasts once: a run that ended unclean, a game updated, and an install without script_docs
const toasted = new Set<string>();
watch(
  [crashed, state],
  ([wasCrashed, current]) => {
    if (THIS_WINDOW !== "main") return;
    if (wasCrashed) {
      showToast("crashed", {
        title: "PMT closed unexpectedly",
        description:
          "The last session ended without closing. Copy diagnostics and paste them into a bug report on GitHub.",
        color: "warning",
        icon: "i-lucide-triangle-alert",
        actions: [{ label: "Copy diagnostics", color: "warning", onClick: () => copyDiagnostics() }],
      });
    }
    if (!current?.open) return;
    if (current.updatedFrom) {
      showToast(`updated ${current.install.id}`, {
        title: "Game updated",
        description: `Game updated from ${current.updatedFrom} to ${current.install.version} — rescanned.`,
        color: "warning",
        icon: "i-lucide-refresh-cw",
      });
    }
    if (current.dumps.origin === "none") {
      showToast(`dumps ${current.install.id}`, {
        title: "No script_docs for this game",
        description:
          "Definitions, references, conflicts and localization still work. Scope-aware completion, typed hover and scope diagnostics need the game to print its documentation once.",
        color: "warning",
        icon: "i-lucide-triangle-alert",
      });
    }
  },
  { immediate: true },
);

/** showToast shows a toast that stays until it is closed, unless the same one was shown before. */
function showToast(key: string, options: Partial<ToastProps>): void {
  if (toasted.has(key)) return;
  toasted.add(key);
  toast.add({ ...options, duration: 0 });
}

const ITEM = "flex h-6 items-center gap-1.25 hover:text-highlighted";
</script>

<template>
  <footer
    class="flex h-6 shrink-0 items-center gap-3.5 border-t border-default bg-muted px-3 text-11 whitespace-nowrap text-dimmed"
  >
    <template v-if="scan || state?.open">
      <span class="flex items-center gap-1.5 text-toned">
        <span
          class="size-1.5 shrink-0 rounded-full"
          :class="[state?.installFound === false ? 'bg-error' : 'bg-success', { 'animate-pulse': scan }]"
        />
        <template v-if="scan">
          <span>{{ scan.stage }}</span>
          <span class="tabular-nums">{{ scan.percent }}%</span>
        </template>
        <span v-else-if="state?.installFound === false">Install not found</span>
        <span v-else-if="state?.open">
          {{ installLine(state.game?.shortName ?? "", state.install.version, state.install.scannedAt) }}
        </span>
      </span>
      <!-- the line before the page's own facts, shown when the page has some -->
      <span class="hidden h-3 w-px shrink-0 bg-accented [&:has(+#status-start>*)]:block" />
    </template>
    <div id="status-start" class="contents" />
    <span class="grow" />
    <div id="status-end" class="contents" />
    <span v-if="wiki" class="flex items-center gap-1.25 tabular-nums">
      <span class="size-1.5 shrink-0 animate-pulse rounded-full bg-primary" />
      {{ state?.game?.name ?? "Game" }} wiki: {{ wiki.done }} of {{ wiki.total }}
    </span>
    <UDropdownMenu v-if="THIS_WINDOW === 'main' && others.length" :items="others" :content="{ side: 'top' }">
      <button type="button" :class="ITEM">
        <UIcon name="i-lucide-app-window" class="size-3" />{{ others.length + 1 }} windows
      </button>
    </UDropdownMenu>
    <UModal :title="`Help: ${page?.title ?? 'PMT'}`" :ui="{ content: 'h-118 sm:max-w-2xl' }">
      <button type="button" :class="ITEM">Help</button>
      <template #body>
        <div class="flex flex-col gap-3 text-13 text-toned">
          <p v-for="(paragraph, index) in page?.help ?? []" :key="index">{{ paragraph }}</p>
        </div>
      </template>
      <template #footer>
        <span class="grow text-12 text-dimmed">Found a bug? Copy diagnostics and paste them into a GitHub issue.</span>
        <UButton label="Copy diagnostics" icon="i-lucide-clipboard-copy" @click="copyDiagnostics()" />
      </template>
    </UModal>
    <UModal
      title="Releases"
      :ui="{ content: 'h-130 sm:max-w-2xl' }"
      @update:open="(opened) => opened && refetchReleases()"
    >
      <UTooltip v-if="update?.available" text="A newer version is out">
        <UButton :label="version" icon="i-lucide-download" size="xs" color="primary" variant="soft" />
      </UTooltip>
      <button v-else type="button" :class="ITEM">{{ version }}</button>
      <template #body>
        <div class="flex flex-col gap-5">
          <section v-for="release in releases ?? []" :key="release.version" class="flex flex-col gap-1.5">
            <h3 class="text-13 font-semibold text-highlighted">
              {{ release.version }} <span class="font-normal text-dimmed">{{ day(release.date) }}</span>
            </h3>
            <UEditor :model-value="release.notes" content-type="markdown" :editable="false" :mention="false" />
          </section>
        </div>
      </template>
      <template #footer>
        <span class="grow" />
        <UButton v-if="update?.available" :label="`Update to ${update.version}`" @click="applyUpdate()" />
        <UButton v-else label="Check for updates" :loading="checking" @click="checkUpdate()" />
      </template>
    </UModal>
  </footer>
</template>
