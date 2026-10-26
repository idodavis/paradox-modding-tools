<!--
  The top bar: the workspace switcher, the workspace's page tabs, and the palette, new window, light and dark, and
  Settings buttons.
-->
<script setup lang="ts">
import type { ContextMenuItem, DropdownMenuItem } from "@nuxt/ui";
import { useQuery } from "@pinia/colada";
import { useElementSize, useEventBus } from "@vueuse/core";
import { Events } from "@wailsio/runtime";
import { computed, onScopeDispose, shallowRef, useTemplateRef } from "vue";
import { useRoute, useRouter } from "vue-router";

import logoIcon from "@assets/PMT-Logo-Icon.svg?url";
import { installLine } from "../lib/format";
import { gamesQuery, installsQuery, stateQuery, workspacesQuery } from "../queries";
import { gameIcon, useSettingsStore } from "../stores/settings";
import { THIS_WINDOW, useWindowsStore } from "../stores/windows";
import { useWorkspaceStore } from "../stores/workspace";
import { homeRoute, isWorkspacePage, listShownTools, PAGES, type WorkspaceTool } from "./pages";

const route = useRoute();
const router = useRouter();
const workspace = useWorkspaceStore();
const settings = useSettingsStore();
const windows = useWindowsStore();
const bus = useEventBus<string>("pmt.command");

const { data: workspaces } = useQuery(workspacesQuery);
const { data: games } = useQuery(gamesQuery);
const { data: installs } = useQuery(installsQuery);
const { data: state } = useQuery(stateQuery);

const TAB = "flex h-11 items-center gap-1.5 px-2.5 -mb-px border-b-2 text-13 font-medium whitespace-nowrap";

const active = computed(() => workspaces.value?.find((record) => record.id === workspace.activeId()));
const tools = computed(listShownTools);
/** Finds the id of the game of an install, or "" when the install is not known. */
const findGame = (installId: string) => installs.value?.find((install) => install.id === installId)?.game ?? "";

// a narrow window keeps the inactive tabs' icons
const bar = useTemplateRef("bar");
const { width } = useElementSize(bar);
const compact = computed(
  () => width.value > 0 && width.value < (1220 * (Number(settings.value("appearance.scale")) || 100)) / 100,
);

// a page in a window of its own: its tab brings that window up, and the main window shows another page
/** The window showing a page, or "" when none does. */
const holder = (name: WorkspaceTool) => windows.holderOf({ page: name });
/** Whether a page is shown in a window of its own. */
const popped = (name: WorkspaceTool) => !!holder(name) && holder(name) !== THIS_WINDOW;
/** Whether a page can open in a window of its own. The IDE stays in the main window. */
const canPopOut = (name: WorkspaceTool) => name !== "workspace-ide";
// the page shown here, when it can open in a window of its own
const page = computed(() => tools.value.find((tool) => tool === route.name && canPopOut(tool)));

/** tabClass returns the classes of a page's tab, marked when its page is the one shown. */
function tabClass(on: boolean): string[] {
  return [TAB, on ? "border-primary text-highlighted" : "border-transparent text-toned hover:text-highlighted"];
}

/** popOut opens a page in a window of its own, and shows another page here if it was the one shown. */
function popOut(name: WorkspaceTool): void {
  windows.popOut({ page: name });
  const next = tools.value.find((tool) => tool !== name && !popped(tool));
  if (route.name === name && next) void router.push({ name: next, params: { id: workspace.activeId() } });
}

/** closePage closes a page's window and shows the page here again. */
function closePage(name: WorkspaceTool): void {
  windows.closeOther(holder(name));
  void router.push({ name, params: { id: workspace.activeId() } });
}

/** tabMenu returns a tab's context menu: the IDE stays in the main window, a page opens in a window of its own. */
function tabMenu(name: WorkspaceTool): ContextMenuItem[] {
  if (!canPopOut(name)) return [];
  if (!popped(name))
    return [{ label: "Open in a new window", icon: "i-lucide-app-window", onSelect: () => popOut(name) }];
  return [
    { label: "Show window", icon: "i-lucide-app-window", onSelect: () => windows.showWindow(holder(name)) },
    { label: "Back into the main window", icon: "i-lucide-panel-top-close", onSelect: () => closePage(name) },
  ];
}

/** resolveLink returns a workspace's link in the switcher: the page open now, where it is one of a workspace's. */
function resolveLink(id: string) {
  const name = route.name;
  if (name && isWorkspacePage(name)) return { name, params: { id } };
  return homeRoute(id, workspaces.value?.find((record) => record.id === id)?.homePage ?? "");
}

/** The switcher's menu: each game's workspaces, then New workspace. */
const switcher = computed(
  () =>
    [
      (games.value ?? []).flatMap((game) => {
        const rows = (workspaces.value ?? []).filter((record) => game && findGame(record.install) === game.id);
        if (!game || !rows.length) return [];
        return [
          { type: "label" as const, label: game.name, slot: "game" as const, game: game.id },
          ...rows.map((record) => ({
            label: record.name,
            icon: record.id === workspace.activeId() ? "i-lucide-check" : undefined,
            to: resolveLink(record.id),
          })),
        ];
      }),
      [{ label: "New workspace", icon: "i-lucide-plus", to: { name: "wizard" as const } }],
    ] satisfies DropdownMenuItem[][],
);

// the light and dark button offers the colors the theme is not in
const mode = computed(() =>
  settings.theme().dark
    ? { label: "Switch to light", icon: "i-lucide-sun" }
    : { label: "Switch to dark", icon: "i-lucide-moon" },
);

/** writeDark writes the theme's other colors, dark or light, which stops following the system's. */
function writeDark(): void {
  settings.write("appearance.followSystem", "false");
  settings.write("appearance.dark", String(!settings.theme().dark));
}

// the install's scan while a workspace opens, as the last stage it reported
const scan = shallowRef<{ stage: string; percent: number } | null>(null);
onScopeDispose(Events.On("install:scanning", ({ data }) => (scan.value = data)));
for (const event of ["install:scanned", "workspace:opened", "workspace:failed"] as const) {
  onScopeDispose(Events.On(event, () => (scan.value = null)));
}
</script>

<template>
  <header ref="bar" class="flex h-11 shrink-0 items-center gap-3 border-b border-default bg-muted ps-3.5 pe-3">
    <RouterLink
      :to="{ name: 'library' }"
      aria-label="PMT, go to Library"
      class="-mb-px flex h-11 shrink-0 items-center gap-2 border-b-2 px-1 text-13 font-semibold text-highlighted"
      :class="route.name === 'library' || route.name === 'wizard' ? 'border-primary' : 'border-transparent'"
      data-art
    >
      <span
        class="size-4.5 bg-primary"
        :style="{ maskImage: `url(${logoIcon})`, maskRepeat: 'no-repeat', maskPosition: 'center', maskSize: 'contain' }"
      />
      PMT
    </RouterLink>
    <span class="mx-1 h-4.5 w-px shrink-0 bg-accented" />

    <UDropdownMenu :items="switcher" :content="{ align: 'start' }">
      <template #game-leading="{ item }">
        <img :src="gameIcon(item.game)" :alt="item.game" class="size-4.5 shrink-0 rounded-sm object-contain" data-art />
      </template>
      <UButton
        size="sm"
        class="w-56 shrink-0 gap-2 ps-1.5 pe-2 text-13"
        trailing-icon="i-lucide-chevron-down"
        :ui="{ trailingIcon: 'text-dimmed' }"
      >
        <img
          v-if="active && gameIcon(findGame(active.install))"
          :src="gameIcon(findGame(active.install))"
          :alt="findGame(active.install)"
          class="size-4.5 shrink-0 rounded-sm object-contain"
          data-art
        />
        <span class="min-w-0 flex-1 truncate text-start">{{ active?.name ?? "Choose a workspace" }}</span>
      </UButton>
    </UDropdownMenu>

    <nav v-if="active" class="ms-1 flex h-11 min-w-0 items-center gap-0.5">
      <UContextMenu v-for="name in tools" :key="name" :items="tabMenu(name)" :disabled="!canPopOut(name)">
        <button
          v-if="popped(name)"
          type="button"
          :class="tabClass(false)"
          :aria-label="`${PAGES[name].title}, in its own window`"
          @click="windows.showWindow(holder(name))"
        >
          <UIcon :name="PAGES[name].icon" class="size-4" />
          <span v-if="!compact">{{ PAGES[name].title }}</span>
          <UIcon name="i-lucide-app-window" class="size-3.5 text-dimmed" />
        </button>
        <RouterLink
          v-else
          :to="{ name, params: { id: workspace.activeId() } }"
          :class="tabClass(route.name === name)"
          :aria-label="PAGES[name].title"
        >
          <UIcon :name="PAGES[name].icon" class="size-4" />
          <span v-if="!compact || route.name === name">{{ PAGES[name].title }}</span>
        </RouterLink>
      </UContextMenu>
    </nav>

    <div class="ms-auto flex shrink-0 items-center gap-1.5">
      <div
        v-if="scan || state?.open"
        class="flex h-7 items-center gap-1.5 rounded-md border border-default bg-elevated ps-2 pe-2.5 text-12 whitespace-nowrap text-toned"
      >
        <span
          class="size-1.75 shrink-0 rounded-full"
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
      </div>
      <UTooltip text="Search and commands" :kbds="['meta', 'K']">
        <UButton
          icon="i-lucide-search"
          variant="ghost"
          size="sm"
          square
          aria-label="Search and commands"
          @click="bus.emit('palette.open')"
        />
      </UTooltip>
      <UTooltip v-if="page" text="Open this page in a new window">
        <UButton
          icon="i-lucide-app-window"
          variant="ghost"
          size="sm"
          square
          aria-label="Open this page in a new window"
          @click="popOut(page)"
        />
      </UTooltip>
      <UTooltip :text="mode.label">
        <UButton :icon="mode.icon" variant="ghost" size="sm" square :aria-label="mode.label" @click="writeDark()" />
      </UTooltip>
      <UTooltip text="Settings">
        <UButton
          icon="i-lucide-settings"
          :variant="route.name === 'workspace-settings' ? 'soft' : 'ghost'"
          size="sm"
          square
          aria-label="Settings"
          :disabled="!active"
          :to="active ? { name: 'workspace-settings', params: { id: active.id } } : undefined"
        />
      </UTooltip>
    </div>
  </header>
</template>
