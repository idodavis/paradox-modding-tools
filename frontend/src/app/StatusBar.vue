<!--
  The status bar: the install and its scan, the page's facts, the agent, the windows and the version. It also holds
  the Help dialog, which the top bar's gear menu opens.
-->
<script setup lang="ts">
import type { DropdownMenuItem, ToastProps } from "@nuxt/ui";
import { useMutation, useQuery } from "@pinia/colada";
import { useEventBus, useIntervalFn } from "@vueuse/core";
import { Events } from "@wailsio/runtime";
import { computed, onScopeDispose, shallowRef, watch } from "vue";
import { useRoute, useRouter } from "vue-router";

import * as app from "@services/app/service";
import type { Release } from "@services/app/models";
import type { Progress } from "@services/wiki/models";
import * as wikiApi from "@services/wiki/service";
import type { Scanning } from "@services/workspace/models";
import { confirm, copyText } from "../lib/feedback";
import { day, installLine, toSentence } from "../lib/format";
import {
  configFaultQuery,
  crashedQuery,
  levelsQuery,
  noticesQuery,
  releasesQuery,
  stateQuery,
  updateQuery,
  useRescan,
  versionQuery,
} from "../queries";
import { useAgentStore } from "../stores/agent";
import { COMMAND_BUS, describeWindow, THIS_WINDOW, useWindowsStore } from "../stores/windows";
import HowModWorks from "../ui/HowModWorks.vue";
import ScriptDocsGuide from "../ui/ScriptDocsGuide.vue";
import { STATUS_ITEM } from "../ui/StatusFacts.vue";
import LICENSE from "../../../LICENSE?raw";
import NOTICE from "../../../NOTICE?raw";
import TermInfo from "../ui/TermInfo.vue";
import { findCommand } from "./commands";
import { isWorkspacePage, PAGES, PANE_HELP, TERMS, type Page } from "./pages";

const route = useRoute();
const router = useRouter();
const toast = useToast();
const windows = useWindowsStore();
const agent = useAgentStore();

const page = computed<Page | undefined>(() => (route.name ? PAGES[route.name] : undefined));
// A workspace page's help is followed by the agent pane's.
const sections = computed(() => [
  ...(page.value?.help ?? []),
  ...(route.name && isWorkspacePage(route.name) ? PANE_HELP : []),
]);
// The Help dialog opens on the help.open command, from the top bar's gear menu, F1 or the palette.
const helping = shallowRef(false);
const explaining = shallowRef(false);
// the one guide to the game's documentation: nested in another dialog, Reka's layers update each other without end
const guiding = shallowRef(false);
const licensing = shallowRef(false);
useEventBus(COMMAND_BUS).on((command) => {
  if (command === "help.open") helping.value = true;
  if (command === "docs.guide") guiding.value = true;
  if (command === "help.licenses") licensing.value = true;
});
const { data: notices } = useQuery(() => ({ ...noticesQuery, enabled: licensing.value }));
const { mutate: rescan } = useRescan();
const installMenu = computed<DropdownMenuItem[]>(() => [
  {
    label: "Rescan the install",
    icon: "i-lucide-refresh-cw",
    onSelect: () => state.value && rescan(state.value.install.id),
  },
  { label: "How to get the game's documentation", icon: "i-lucide-book-open", onSelect: () => (guiding.value = true) },
]);
const agentDot = computed(() => {
  if (!agent.active) return "";
  if (agent.isWaiting(agent.active)) return "bg-warning";
  return agent.active.state === "running" ? "animate-pulse bg-primary" : "bg-success";
});
const { data: state } = useQuery(stateQuery);
const { data: version } = useQuery(versionQuery);
// Only the main window checks for updates, at launch and then every six hours, as GitHub allows few requests an hour.
const CHECK_INTERVAL = 6 * 3_600_000;
const {
  data: update,
  error: checkError,
  refetch: checkUpdate,
  isLoading: checking,
} = useQuery({ ...updateQuery, staleTime: CHECK_INTERVAL, enabled: THIS_WINDOW === "main" });
if (THIS_WINDOW === "main") useIntervalFn(() => void checkUpdate(), CHECK_INTERVAL);
const { data: releases, error: listError, refetch: refetchReleases } = useQuery({ ...releasesQuery, enabled: false });
const { data: crashed } = useQuery(crashedQuery);
const { data: configFault } = useQuery(configFaultQuery);
const { data: levels } = useQuery(levelsQuery);
const terms = computed(() => [
  ...TERMS,
  ...(levels.value ?? []).map((level) => ({ term: level.id, meaning: level.cost })),
]);
const { mutate: applyUpdate } = useMutation({ mutation: () => app.ApplyUpdate() });
const { mutate: installRelease } = useMutation({ mutation: (target: string) => app.InstallRelease(target) });

/**
 * installVersion installs a release and restarts into it. It asks first for an older one, which can leave the user's
 * settings unread and keeps the running version from being offered until a newer one is out.
 */
async function installVersion(release: Release): Promise<void> {
  const target = release.version;
  if (!release.older) {
    installRelease(target);
    return;
  }
  const confirmed = await confirm.open({
    title: `Install ${target}?`,
    body:
      `PMT restarts into ${target}. Your settings and workspaces stay. If ${target} cannot read them, it starts empty ` +
      `and leaves them alone until you update again. Update passes ${version.value} by until a newer version is out.`,
    confirm: `Install ${target}`,
  });
  if (confirmed) installRelease(target);
}
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
  [crashed, configFault, state],
  ([wasCrashed, fault, current]) => {
    if (THIS_WINDOW !== "main") return;
    if (fault) {
      showToast("config-fault", {
        title: "Your settings and workspaces could not be read",
        description:
          "PMT started empty and leaves config.json as it is, so nothing you change is saved. Mend or move the " +
          `file, then start PMT again. ${fault}`,
        color: "error",
        icon: "i-lucide-triangle-alert",
      });
    }
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
        actions: [
          {
            label: "Open Patch Notes",
            color: "warning",
            onClick: () => void router.push({ name: "patch-notes", params: { id: current.id } }),
          },
        ],
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

const LAUNCH = findCommand("game.launch");
</script>

<template>
  <footer
    class="flex h-6 shrink-0 items-center gap-3.5 border-t border-default bg-muted px-3 text-11 whitespace-nowrap text-dimmed"
  >
    <template v-if="scan || state?.open">
      <UDropdownMenu :items="installMenu" :disabled="!!scan" :content="{ side: 'top', align: 'start' }">
        <button type="button" :class="[STATUS_ITEM, 'text-toned']">
          <span
            class="size-1.5 shrink-0 rounded-full"
            :class="[state?.installFound === false ? 'bg-error' : 'bg-success', { 'animate-pulse': scan }]"
          />
          <template v-if="scan">
            <span>{{ scan.stage }}</span>
            <span v-if="scan.percent >= 0" class="tabular-nums">{{ scan.percent }}%</span>
          </template>
          <span v-else-if="state?.installFound === false">Install not found</span>
          <span v-else-if="state?.open">
            {{ installLine(state.game?.shortName ?? "", state.install.version, state.install.scannedAt) }}
          </span>
        </button>
      </UDropdownMenu>
      <UTooltip
        v-if="state?.open && !scan && state.installFound !== false"
        :text="`Launch ${state.game?.name} in debug mode. It writes fresh documentation for PMT.`"
      >
        <button
          type="button"
          :class="STATUS_ITEM"
          :aria-label="`Launch ${state.game?.name} in debug mode`"
          @click="LAUNCH?.onSelect?.()"
        >
          <UIcon name="i-lucide-play" class="size-3" />
        </button>
      </UTooltip>
      <span class="hidden h-3 w-px shrink-0 bg-accented [&:has(+#status-start>*)]:block" />
    </template>
    <div id="status-start" class="contents" />
    <span class="grow" />
    <div id="status-end" class="contents" />
    <button v-if="agent.status && agent.active" type="button" :class="STATUS_ITEM" @click="agent.showPane()">
      <span class="size-1.5 shrink-0 rounded-full" :class="agentDot" />
      {{ agent.status }}
    </button>
    <span v-if="wiki" class="flex items-center gap-1.25 tabular-nums">
      <span class="size-1.5 shrink-0 animate-pulse rounded-full bg-primary" />
      {{ state?.game?.name ?? "Game" }} wiki: {{ wiki.done }} of {{ wiki.total }}
    </span>
    <UPopover v-if="wikiFailure && !wiki" :content="{ side: 'top' }">
      <button type="button" :class="[STATUS_ITEM, 'text-warning']">
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
      <button type="button" :class="STATUS_ITEM">
        <UIcon name="i-lucide-app-window" class="size-3" />{{ others.length + 1 }} windows
      </button>
    </UDropdownMenu>
    <UModal v-model:open="helping" :title="`Help: ${page?.title ?? 'PMT'}`" :ui="{ content: 'h-[80vh] sm:max-w-4xl' }">
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
          <section v-for="section in sections" :key="section.heading" class="flex flex-col gap-1">
            <h3 class="text-13 font-semibold text-highlighted">{{ section.heading }}</h3>
            <p>{{ section.text }}</p>
          </section>
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
      v-model:open="licensing"
      title="Licenses"
      description="PMT's own license, then the packages it is made with, each used under the license shown."
      :ui="{ content: 'h-[80vh] sm:max-w-4xl' }"
    >
      <template #body>
        <div class="flex flex-col gap-6">
          <pre class="font-mono text-12 whitespace-pre-wrap text-toned">{{ NOTICE }}{{ "\n" }}{{ LICENSE }}</pre>
          <pre v-if="notices" class="font-mono text-12 whitespace-pre-wrap text-toned">{{ notices }}</pre>
          <p v-else class="text-13 text-dimmed">
            A release build lists the third-party packages here. This build was made without them.
          </p>
        </div>
      </template>
    </UModal>
    <!-- mounted on opening, so it lands after any dialog already open and shows above it -->
    <ScriptDocsGuide v-if="guiding" v-model:open="guiding" :game="state?.game?.id ?? ''" />
    <UModal
      title="Releases"
      :ui="{ content: 'h-160 sm:max-w-3xl' }"
      @update:open="(opened) => opened && refetchReleases()"
    >
      <UTooltip :text="update?.available ? 'Releases and updates: a newer version is out' : 'Releases and updates'">
        <UButton
          v-if="update?.available"
          :label="version"
          icon="i-lucide-download"
          size="xs"
          color="primary"
          variant="soft"
        />
        <button v-else type="button" :class="STATUS_ITEM">
          <UIcon name="i-lucide-tag" class="size-3" />{{ version }}
        </button>
      </UTooltip>
      <template #body>
        <div class="flex flex-col gap-5">
          <p v-if="listError" class="text-13 text-error">
            The releases could not be read. {{ toSentence(listError.message) }}
          </p>
          <section v-for="release in releases ?? []" :key="release.version" class="flex flex-col gap-1.5">
            <div class="flex items-center gap-2">
              <h3 class="text-13 font-semibold text-highlighted">
                {{ release.version }} <span class="font-normal text-dimmed">{{ day(release.date) }}</span>
              </h3>
              <UBadge v-if="release.running" label="Running" color="primary" variant="subtle" size="sm" />
              <UButton
                v-else-if="release.installable"
                label="Install this version"
                size="xs"
                variant="soft"
                class="ml-auto"
                @click="installVersion(release)"
              />
            </div>
            <UEditor :model-value="release.notes" content-type="markdown" :editable="false" :mention="false" />
          </section>
        </div>
      </template>
      <template #footer>
        <span class="grow text-12 text-error">
          {{ checkError && `The check for updates failed. ${toSentence(checkError.message)}` }}
        </span>
        <UButton v-if="update?.available" :label="`Update to ${update.version}`" @click="applyUpdate()" />
        <UButton v-else label="Check for updates" :loading="checking" @click="checkUpdate()" />
      </template>
    </UModal>
  </footer>
</template>
