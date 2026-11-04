<!-- The status bar: the install and its scan, the page's facts, the agent, the windows, Help and the version. -->
<script setup lang="ts">
import type { ToastProps } from "@nuxt/ui";
import { useMutation, useQuery } from "@pinia/colada";
import { Events } from "@wailsio/runtime";
import { computed, onScopeDispose, shallowRef, watch } from "vue";
import { useRoute } from "vue-router";

import * as app from "@services/app/service";
import type { Progress } from "@services/wiki/models";
import * as wikiApi from "@services/wiki/service";
import type { Scanning } from "@services/workspace/models";
import { copyText } from "../lib/feedback";
import { day, installLine } from "../lib/format";
import { crashedQuery, levelsQuery, releasesQuery, stateQuery, updateQuery, versionQuery } from "../queries";
import { useAgentStore } from "../stores/agent";
import { describeWindow, THIS_WINDOW, useWindowsStore } from "../stores/windows";
import HowModWorks from "../ui/HowModWorks.vue";
import TermInfo from "../ui/TermInfo.vue";
import { isWorkspacePage, PAGES, PANE_HELP, TERMS, type Page } from "./pages";

const route = useRoute();
const toast = useToast();
const windows = useWindowsStore();
const agent = useAgentStore();

const page = computed<Page | undefined>(() => (route.name ? PAGES[route.name] : undefined));
const explaining = shallowRef(false);
const agentDot = computed(() => {
  if (!agent.active) return "";
  if (agent.isWaiting(agent.active)) return "bg-warning";
  return agent.active.state === "running" ? "animate-pulse bg-primary" : "bg-success";
});
const { data: state } = useQuery(stateQuery);
const { data: version } = useQuery(versionQuery);
const { data: update, refetch: checkUpdate, isLoading: checking } = useQuery({ ...updateQuery, staleTime: 3_600_000 });
const { data: releases, refetch: refetchReleases } = useQuery({ ...releasesQuery, enabled: false });
const { data: crashed } = useQuery(crashedQuery);
const { data: levels } = useQuery(levelsQuery);
const terms = computed(() => [
  ...TERMS,
  ...(levels.value ?? []).map((level) => ({ term: level.id, meaning: level.cost })),
]);
const { mutate: applyUpdate } = useMutation({ mutation: () => app.ApplyUpdate() });
const { mutate: copyDiagnostics } = useMutation({
  mutation: async () =>
    copyText(await app.BuildDiagnostics(), "Diagnostics", "Paste them into a bug report on GitHub."),
});

const wiki = shallowRef<Progress | null>(null);
const wikiFailure = shallowRef("");
onScopeDispose(Events.On("wiki:progress", ({ data }) => (wiki.value = data)));
onScopeDispose(
  Events.On("wiki:updated", ({ data }) => {
    wiki.value = null;
    wikiFailure.value = data;
  }),
);

const scan = shallowRef<Scanning | null>(null);
onScopeDispose(Events.On("install:scanning", ({ data }) => (scan.value = data)));
for (const event of ["install:scanned", "workspace:opened", "workspace:failed"] as const) {
  onScopeDispose(Events.On(event, () => (scan.value = null)));
}

const others = computed(() =>
  windows
    .listWindows()
    .filter((name) => name !== THIS_WINDOW)
    .map((name) => ({ label: describeWindow(name), onSelect: () => windows.showWindow(name) })),
);

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
      <span class="hidden h-3 w-px shrink-0 bg-accented [&:has(+#status-start>*)]:block" />
    </template>
    <div id="status-start" class="contents" />
    <span class="grow" />
    <div id="status-end" class="contents" />
    <button v-if="agent.status && agent.active" type="button" :class="ITEM" @click="agent.showPane()">
      <span class="size-1.5 shrink-0 rounded-full" :class="agentDot" />
      {{ agent.status }}
    </button>
    <span v-if="wiki" class="flex items-center gap-1.25 tabular-nums">
      <span class="size-1.5 shrink-0 animate-pulse rounded-full bg-primary" />
      {{ state?.game?.name ?? "Game" }} wiki: {{ wiki.done }} of {{ wiki.total }}
    </span>
    <UPopover v-if="wikiFailure && !wiki" :content="{ side: 'top' }">
      <button type="button" :class="[ITEM, 'text-warning']">
        <UIcon name="i-lucide-triangle-alert" class="size-3" />Wiki not fetched
      </button>
      <template #content>
        <div class="flex max-w-80 flex-col items-start gap-2 p-3 text-12 whitespace-normal">
          <p class="text-toned">{{ wikiFailure }}</p>
          <UButton label="Retry" size="xs" @click="wikiApi.Refresh()" />
        </div>
      </template>
    </UPopover>
    <UDropdownMenu v-if="THIS_WINDOW === 'main' && others.length" :items="others" :content="{ side: 'top' }">
      <button type="button" :class="ITEM">
        <UIcon name="i-lucide-app-window" class="size-3" />{{ others.length + 1 }} windows
      </button>
    </UDropdownMenu>
    <UModal :title="`Help: ${page?.title ?? 'PMT'}`" :ui="{ content: 'h-118 sm:max-w-2xl' }">
      <button type="button" :class="ITEM">Help</button>
      <template #body>
        <div class="flex flex-col gap-3 text-13 text-toned">
          <section class="flex flex-col gap-3 rounded-lg bg-elevated p-3.5">
            <template v-if="page?.start">
              <h3 class="text-13 font-semibold text-highlighted">Start here</h3>
              <ol class="flex flex-col gap-2.5">
                <li v-for="(step, index) in page.start" :key="index" class="flex items-start gap-2.5">
                  <span
                    class="mt-0.5 flex size-5 shrink-0 items-center justify-center rounded-full bg-accented text-11 font-medium"
                  >
                    {{ index + 1 }}
                  </span>
                  <span>{{ step }}</span>
                </li>
              </ol>
            </template>
            <div class="flex flex-col gap-2" :class="page?.start && 'border-t border-default pt-2.5'">
              <span class="text-12 text-dimmed">Words PMT uses</span>
              <div class="flex flex-wrap gap-1.5">
                <TermInfo v-for="row in terms" :key="row.term" :term="row.term" :meaning="row.meaning">
                  <UBadge :label="row.term" />
                </TermInfo>
              </div>
              <span class="text-11 text-dimmed">The same words show beside an (i) on the pages that use them.</span>
            </div>
          </section>
          <p v-for="(paragraph, index) in page?.help ?? []" :key="index">{{ paragraph }}</p>
          <template v-if="route.name && isWorkspacePage(route.name)">
            <h3 class="mt-2 text-13 font-semibold text-highlighted">The agent pane</h3>
            <p v-for="(paragraph, index) in PANE_HELP" :key="`pane${index}`">{{ paragraph }}</p>
          </template>
          <UButton label="How a mod works" icon="i-lucide-layers" class="mt-2 self-start" @click="explaining = true" />
        </div>
      </template>
      <template #footer>
        <span class="grow text-12 text-dimmed">Found a bug? Copy diagnostics and paste them into a GitHub issue.</span>
        <UButton label="Copy diagnostics" icon="i-lucide-clipboard-copy" @click="copyDiagnostics()" />
      </template>
    </UModal>
    <HowModWorks v-model:open="explaining" :game-id="state?.game?.id ?? ''" mod-title="" />
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
