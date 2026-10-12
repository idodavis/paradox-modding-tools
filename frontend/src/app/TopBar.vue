<script setup lang="ts">
/**
 * The top bar on every page (the boards' .topbar): PMT and the Library, the workspace switcher and the
 * workspace's page tabs, then its scan, search, light and dark, and Settings. On a workspace's page it
 * builds the workspace's session. A page but the IDE opens in a window of its own from its tab's menu; its tab then
 * brings that window up, and its menu brings the page back.
 */
import { computed, useTemplateRef } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useQuery } from "@pinia/colada";
import { until, useElementSize, useTimeAgo } from "@vueuse/core";
import type { ContextMenuItem, DropdownMenuItem } from "@nuxt/ui";
import logoIcon from "@assets/PMT-Logo-Icon.svg?url";
import { useCommandsStore } from "../stores/commands";
import { useLanguageHealth } from "../composables/useLanguageHealth";
import { sessionQuery, workspacesQuery } from "../queries";
import { useSettingsStore } from "../stores/settings";
import { useWorkspaceStore } from "../stores/workspace";
import { Close, Show } from "@services/windowservice";
import { useWindowsStore } from "../stores/windows";
import { isWorkspacePage, WORKSPACE_TOOLS, workspaceHomeRoute, type WorkspaceToolName } from "./pages";
import GameIcon from "../ui/GameIcon.vue";

const route = useRoute();
const store = useWorkspaceStore();
const palette = useCommandsStore();
const settings = useSettingsStore();

const { data: workspaces } = useQuery(workspacesQuery);
// the open workspace's session, built on its pages only (the router's guard makes it the active one); none on the
// Library and the wizard
useQuery(() => sessionQuery(route.name && isWorkspacePage(route.name) ? store.activeWorkspaceId : ""));
const { health, scan } = useLanguageHealth(() => store.activeWorkspaceId);

const TAB = "flex h-11 items-center gap-1.5 px-2.5 -mb-px border-b-2 text-13 font-medium whitespace-nowrap";
function tabClass(on: boolean): string[] {
  return [TAB, on ? "border-primary text-highlighted" : "border-transparent text-toned hover:text-highlighted"];
}

const tabs = computed(() => WORKSPACE_TOOLS.filter((tool) => settings.visibleTools.includes(tool.name)));

// a page in a window of its own: its tab brings that window up, and the main window shows another page; back, the
// window closes and the page shows here
const router = useRouter();
const windows = useWindowsStore();
const popped = (name: WorkspaceToolName) => windows.otherWindows.has(`page:${name}`);
async function popOut(name: WorkspaceToolName, label: string): Promise<void> {
  const shown = route.name === name;
  await windows.openWindow(
    shown ? route.fullPath : router.resolve({ name, params: { id: store.activeWorkspaceId } }).fullPath,
    `page:${name}`,
    label,
  );
  const next = tabs.value.find((tool) => tool.name !== name && !popped(tool.name));
  if (shown && next) void router.push({ name: next.name, params: { id: store.activeWorkspaceId } });
}
async function bringBack(name: WorkspaceToolName): Promise<void> {
  void Close(`page:${name}`);
  await until(() => popped(name)).toBe(false, { timeout: 5000 });
  void router.push({ name, params: { id: store.activeWorkspaceId } });
}
function tabMenu(tool: { name: WorkspaceToolName; label: string }): ContextMenuItem[] {
  if (tool.name === "workspace-ide") return [];
  if (!popped(tool.name))
    return [
      {
        label: "Open in a new window",
        icon: "i-lucide-app-window",
        onSelect: () => void popOut(tool.name, tool.label),
      },
    ];
  return [
    { label: "Show window", icon: "i-lucide-app-window", onSelect: () => void Show(`page:${tool.name}`) },
    { label: "Back into the main window", icon: "i-lucide-panel-top-close", onSelect: () => void bringBack(tool.name) },
  ];
}

// a narrow window keeps the inactive tabs' icons and the scan's age (pmt.css .topbar.compact)
const bar = useTemplateRef("bar");
const { width } = useElementSize(bar);
const compact = computed(() => width.value > 0 && width.value < (1220 * settings.fontScale) / 100);

/** A workspace's link from the switcher: the page open now, where it is one of a workspace's. */
function switchTo(id: string) {
  const name = route.name;
  if (name && isWorkspacePage(name)) return { name, params: { id } };
  const ws = workspaces.value?.find((w) => w.id === id);
  return workspaceHomeRoute({ id, defaultTool: ws?.defaultTool }, settings.visibleTools);
}

/** The switcher's menu: each game's workspaces, then New workspace. */
const switcher = computed(
  () =>
    [
      ...store.games
        .map((game) => {
          const rows = (workspaces.value ?? []).filter((w) => w.gameId === game.id);
          return rows.length
            ? [
                { type: "label" as const, label: game.name, slot: "game" as const, gameId: game.id },
                ...rows.map((w) => ({
                  label: w.name,
                  icon: w.id === store.activeWorkspaceId ? "i-lucide-check" : undefined,
                  to: switchTo(w.id),
                })),
              ]
            : [];
        })
        .filter((group) => group.length),
      [{ label: "New workspace", icon: "i-lucide-plus", to: { name: "wizard" as const } }],
    ] satisfies DropdownMenuItem[][],
);

const scanned = useTimeAgo<"second" | "minute" | "hour" | "day" | "week" | "month" | "year">(
  () => health.value?.scannedAt ?? 0,
  {
    messages: {
      justNow: "just now",
      past: (n: string) => (n.match(/\d/) ? `${n} ago` : n),
      future: (n: string) => n,
      month: (n: number) => `${n} mo`,
      year: (n: number) => `${n} y`,
      day: (n: number) => `${n} d`,
      week: (n: number) => `${n} wk`,
      hour: (n: number) => `${n} h`,
      minute: (n: number) => `${n} min`,
      second: (n: number) => `${n} s`,
      invalid: "",
    },
  },
);

/** "1.14.2 scanned 4 min ago"; a narrow bar keeps the age. */
const scanText = computed(() =>
  [compact.value ? "" : health.value?.gameVersion, `scanned ${scanned.value}`].filter(Boolean).join(" "),
);
</script>

<template>
  <header ref="bar" class="flex h-11 shrink-0 items-center gap-3 border-b border-default bg-muted ps-3.5 pe-3">
    <!-- the brand (pmt.css .brand), the logo's mark 18px in the accent, then PMT: it is the Library's tab -->
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
        <GameIcon :game-id="item.gameId" />
      </template>
      <UButton
        size="sm"
        class="w-56 shrink-0 gap-2 ps-1.5 pe-2 text-13"
        trailing-icon="i-lucide-chevron-down"
        :ui="{ trailingIcon: 'text-dimmed' }"
      >
        <GameIcon v-if="store.activeWorkspace" :game-id="store.activeWorkspace.gameId" />
        <span class="min-w-0 flex-1 truncate text-start">
          {{ store.activeWorkspace?.name ?? "Choose a workspace" }}
        </span>
      </UButton>
    </UDropdownMenu>

    <nav v-if="store.activeWorkspace" class="ms-1 flex h-11 min-w-0 items-center gap-0.5">
      <UContextMenu
        v-for="tool in tabs"
        :key="tool.name"
        :items="tabMenu(tool)"
        :disabled="tool.name === 'workspace-ide'"
      >
        <button
          v-if="popped(tool.name)"
          type="button"
          :class="tabClass(false)"
          :aria-label="`${tool.label}, in its own window`"
          @click="Show(`page:${tool.name}`)"
        >
          <UIcon :name="tool.icon" class="size-4" />
          <span v-if="!compact">{{ tool.label }}</span>
          <UIcon name="i-lucide-app-window" class="size-3.5 text-dimmed" />
        </button>
        <RouterLink
          v-else
          :to="{ name: tool.name, params: { id: store.activeWorkspaceId } }"
          :class="tabClass(route.name === tool.name)"
          :aria-label="tool.label"
        >
          <UIcon :name="tool.icon" class="size-4" />
          <span v-if="!compact || route.name === tool.name">{{ tool.label }}</span>
        </RouterLink>
      </UContextMenu>
    </nav>

    <div class="ms-auto flex shrink-0 items-center gap-1.5">
      <div
        v-if="health"
        class="flex h-7 items-center gap-1.5 rounded-md border border-default bg-elevated ps-2 pe-2.5 text-12 whitespace-nowrap text-toned"
      >
        <span
          class="size-1.75 shrink-0 rounded-full"
          :class="[health.installOk ? 'bg-success' : 'bg-error', { 'animate-pulse': scan }]"
        />
        <template v-if="scan">
          <span>{{ scan.stage }}</span>
          <span class="tabular-nums">{{ scan.percent }}%</span>
        </template>
        <span v-else-if="!health.installOk">Install not found</span>
        <span v-else-if="health.scannedAt">{{ scanText }}</span>
        <span v-else>Not scanned</span>
      </div>
      <UTooltip text="Search and commands" :kbds="['meta', 'K']">
        <UButton
          icon="i-lucide-search"
          variant="ghost"
          size="sm"
          square
          aria-label="Search and commands"
          @click="palette.openPalette()"
        />
      </UTooltip>
      <UTooltip :text="settings.mode === 'dark' ? 'Switch to light' : 'Switch to dark'">
        <UButton
          :icon="settings.mode === 'dark' ? 'i-lucide-sun' : 'i-lucide-moon'"
          variant="ghost"
          size="sm"
          square
          :aria-label="settings.mode === 'dark' ? 'Switch to light' : 'Switch to dark'"
          @click="settings.toggleMode()"
        />
      </UTooltip>
      <UTooltip text="Settings">
        <UButton
          icon="i-lucide-settings"
          :variant="route.name === 'workspace-settings' ? 'soft' : 'ghost'"
          size="sm"
          square
          aria-label="Settings"
          :disabled="!store.activeWorkspace"
          :to="
            store.activeWorkspace ? { name: 'workspace-settings', params: { id: store.activeWorkspaceId } } : undefined
          "
        />
      </UTooltip>
    </div>
  </header>
</template>
