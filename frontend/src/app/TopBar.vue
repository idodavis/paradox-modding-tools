<!--
  The top bar of every window: the PMT menu, then the workspace switcher and the workspace's page tabs in the main
  window or what another window shows, then the agent pane's button in the main window, search and the window's
  buttons. Its empty space moves the window.
-->
<script setup lang="ts">
import type { ContextMenuItem, DropdownMenuItem } from "@nuxt/ui";
import { useQuery } from "@pinia/colada";
import { useDebounceFn, useElementSize, useEventBus, useEventListener } from "@vueuse/core";
import { System, Window } from "@wailsio/runtime";
import { computed, reactive, shallowRef, useTemplateRef, watch } from "vue";
import { useRoute, useRouter } from "vue-router";

import logoIcon from "@assets/PMT-Logo-Icon.svg?url";
import { gamesQuery, workspacesQuery } from "../queries";
import { useAgentStore } from "../stores/agent";
import { gameIcon, useSettingsStore } from "../stores/settings";
import { COMMAND_BUS, describeWindow, THIS_WINDOW, useWindowsStore } from "../stores/windows";
import { useWorkspaceStore } from "../stores/workspace";
import { findKbds } from "./commands";
import { homeRoute, isWorkspacePage, listShownTools, PAGES, type WorkspacePage } from "./pages";

const IS_MAIN = THIS_WINDOW === "main";
// whether the window's buttons are Linux's round ones rather than Windows' square ones
const ROUND = System.IsLinux();
const SQUARE_BUTTON = "flex w-11.5 items-center justify-center text-toned";
const ROUND_BUTTON =
  "flex size-6 items-center justify-center rounded-full bg-accented text-toned hover:text-highlighted";
// a tab's line on top matches its highlight below, so its icon and label center on the bar's middle
const TAB = "flex items-center gap-1.5 px-2.5 border-y-2 border-transparent text-13 font-medium whitespace-nowrap";
// the window moves by this space: Windows reads the region, Linux the drag
const CAPTION = "self-stretch [--wails-draggable:drag] [--wails-non-client-region:caption]";

const route = useRoute();
const router = useRouter();
const workspace = useWorkspaceStore();
const settings = useSettingsStore();
const windows = useWindowsStore();
const agent = useAgentStore();
const commandBus = useEventBus(COMMAND_BUS);

const { data: workspaces } = useQuery(workspacesQuery);
const { data: games } = useQuery(gamesQuery);

const active = computed(() => workspaces.value?.find((row) => row.record.id === workspace.activeId()));
const activeGame = computed(() => active.value?.game?.id ?? "");
const tools = computed(listShownTools);
const activeIcon = computed(() => gameIcon(activeGame.value));

const maximized = shallowRef(false);
const checkMaximized = useDebounceFn(async () => (maximized.value = await Window.IsMaximised()), 100);
useEventListener(window, "resize", () => void checkMaximized());
void checkMaximized();

// the window's buttons in order: the system's name for each, its label, its glyph on a 10 by 10 grid and what it does
const windowButtons = computed(() => [
  { region: "minimize", label: "Minimize", glyph: "M0 5.5h10", press: () => Window.Minimise() },
  {
    region: "maximize",
    label: maximized.value ? "Restore" : "Maximize",
    glyph: maximized.value ? "M2.5 2.5V.5h7v7h-2M.5 2.5h7v7h-7z" : "M.5.5h9v9h-9z",
    press: () => Window.ToggleMaximise(),
  },
  { region: "close", label: "Close", glyph: "M.5.5l9 9M9.5.5l-9 9", press: () => Window.Close() },
]);

// a narrow window keeps the inactive tabs' icons: every label, the pop-out button and the least space to drag by
// take 1110px at a scale of 100%
const bar = useTemplateRef("bar");
const { width } = useElementSize(bar);
const compact = computed(
  () => width.value > 0 && width.value < (1110 * (Number(settings.value("appearance.scale")) || 100)) / 100,
);

// a page in a window of its own: its tab brings that window up, and the main window shows another page
/** pageWindow returns the name of the window of its own that shows a page. */
const pageWindow = (name: WorkspacePage) => `page:${name}`;
/** popped reports whether a page is shown in a window of its own. */
const popped = (name: WorkspacePage) =>
  pageWindow(name) !== THIS_WINDOW && windows.listWindows().includes(pageWindow(name));
/** canPopOut reports whether a page can open in a window of its own. The IDE stays in the main window. */
const canPopOut = (name: WorkspacePage) => name !== "workspace-ide";

// the full path each page was last shown at, by its path without the query
const paths = reactive(new Map<string, string>());
watch(
  () => route.fullPath,
  () => paths.set(route.path, route.fullPath),
  { immediate: true },
);

// the light and dark item offers the colors the theme is not in
const mode = computed(() =>
  settings.theme().dark
    ? { label: "Switch to light", icon: "i-lucide-sun" }
    : { label: "Switch to dark", icon: "i-lucide-moon" },
);

// the PMT menu, in the main window alone: the Library, then Settings, the keyboard shortcuts and light or dark
const menu = computed(
  () =>
    [
      [{ label: "Library", icon: "i-lucide-layout-grid", to: { name: "library" as const } }],
      [
        {
          label: "Settings",
          icon: "i-lucide-settings",
          kbds: findKbds("settings.open"),
          onSelect: () => commandBus.emit("settings.open"),
        },
        {
          label: "Keyboard shortcuts",
          icon: "i-lucide-keyboard",
          onSelect: () => commandBus.emit("settings.open", "shortcuts"),
        },
        { ...mode.value, onSelect: writeDark },
      ],
    ] satisfies DropdownMenuItem[][],
);

// the switcher's menu: each game's workspaces, then New workspace
const switcher = computed(
  () =>
    [
      (games.value ?? []).flatMap((game) => {
        const rows = (workspaces.value ?? []).filter((row) => row.game?.id === game.id);
        if (!rows.length) return [];
        return [
          { type: "label" as const, label: game.name, slot: "game" as const, game: game.id },
          ...rows.map(({ record }) => ({
            label: record.name,
            icon: record.id === workspace.activeId() ? "i-lucide-check" : undefined,
            to: resolveLink(record.id),
          })),
        ];
      }),
      [{ label: "New workspace", icon: "i-lucide-plus", to: { name: "wizard" as const } }],
    ] satisfies DropdownMenuItem[][],
);

/** writeDark writes the theme's other colors, dark or light, which stops following the system's. */
function writeDark(): void {
  settings.write("appearance.followSystem", "false");
  settings.write("appearance.dark", String(!settings.theme().dark));
}

/** resolveLink returns a workspace's link in the switcher: the page open now, where it is one of a workspace's. */
function resolveLink(id: string) {
  const name = route.name;
  if (name && isWorkspacePage(name)) return { name, params: { id } };
  return homeRoute(id, workspaces.value?.find((row) => row.record.id === id)?.record.homePage ?? "");
}

/**
 * resolveTabLink returns where a page's tab goes. It goes back to where the page was last shown, with its view, row
 * or root. The tab of the page shown goes to the page's first state, such as Health's summary.
 */
function resolveTabLink(name: WorkspacePage) {
  const first = { name, params: { id: workspace.activeId() } };
  return route.name === name ? first : (paths.get(router.resolve(first).path) ?? first);
}

/** tabClass returns the classes of a page's tab, marked when its page is the one shown. */
function tabClass(on: boolean): string[] {
  return [TAB, on ? "border-b-primary text-highlighted" : "text-toned hover:text-highlighted"];
}

/** tabMenu returns a tab's context menu: the IDE stays in the main window, a page opens in a window of its own. */
function tabMenu(name: WorkspacePage): ContextMenuItem[] {
  if (!canPopOut(name)) return [];
  if (!popped(name))
    return [{ label: "Open in a new window", icon: "i-lucide-app-window", onSelect: () => popOut(name) }];
  return [
    { label: "Show window", icon: "i-lucide-app-window", onSelect: () => windows.showWindow(pageWindow(name)) },
    {
      label: "Back into the main window",
      icon: "i-lucide-panel-top-close",
      onSelect: () => {
        windows.closeOther(pageWindow(name));
        void router.push({ name, params: { id: workspace.activeId() } });
      },
    },
  ];
}

/** popOut opens a page in a window of its own, and shows another page here if it was the one shown. */
function popOut(name: WorkspacePage): void {
  windows.popOut(pageWindow(name));
  const next = tools.value.find((tool) => tool !== name && !popped(tool));
  if (route.name === name && next) void router.push({ name: next, params: { id: workspace.activeId() } });
}

/** styleButton returns a window button's classes: round on Linux, square on Windows, close red under the pointer. */
function styleButton(region: string): string[] {
  if (ROUND) return [ROUND_BUTTON];
  const hover =
    region === "close" ? "hover:bg-(--window-close) hover:text-white" : "hover:bg-accented hover:text-highlighted";
  return [SQUARE_BUTTON, hover];
}
</script>

<template>
  <!-- the bar's line on top matches its line below, and what fills its height reaches over both -->
  <header
    ref="bar"
    class="flex h-9 shrink-0 items-center gap-3 border-y border-default border-t-transparent bg-muted ps-3.5"
  >
    <UDropdownMenu :items="menu" :content="{ align: 'start' }" :disabled="!IS_MAIN">
      <button
        type="button"
        :aria-label="IS_MAIN ? 'PMT menu' : 'PMT'"
        class="-my-px flex shrink-0 items-center gap-1.5 self-stretch border-y-2 border-transparent px-1 text-13 font-semibold text-highlighted"
        :class="(route.name === 'library' || route.name === 'wizard') && 'border-b-primary'"
        data-art
      >
        <span
          class="size-4.5 bg-secondary"
          :style="{
            maskImage: `url(${logoIcon})`,
            maskRepeat: 'no-repeat',
            maskPosition: 'center',
            maskSize: 'contain',
          }"
        />
        PMT
        <UIcon v-if="IS_MAIN" name="i-lucide-chevron-down" class="size-3.5 text-dimmed" />
      </button>
    </UDropdownMenu>
    <span class="mx-1 h-4.5 w-px shrink-0 bg-accented" />

    <template v-if="IS_MAIN">
      <UDropdownMenu :items="switcher" :content="{ align: 'start' }">
        <template #game-leading="{ item }">
          <img
            :src="gameIcon(item.game)"
            :alt="item.game"
            class="size-4.5 shrink-0 rounded-sm object-contain"
            data-art
          />
        </template>
        <UButton
          size="sm"
          class="w-56 shrink-0 gap-2 ps-1.5 pe-2 text-13"
          trailing-icon="i-lucide-chevron-down"
          :ui="{ trailingIcon: 'text-dimmed' }"
        >
          <img
            v-if="activeIcon"
            :src="activeIcon"
            :alt="activeGame"
            class="size-4.5 shrink-0 rounded-sm object-contain"
            data-art
          />
          <span class="min-w-0 flex-1 truncate text-start">{{ active?.record.name ?? "Choose a workspace" }}</span>
        </UButton>
      </UDropdownMenu>

      <nav v-if="active" class="-my-px ms-1 flex min-w-0 gap-0.5 self-stretch">
        <UContextMenu v-for="name in tools" :key="name" :items="tabMenu(name)" :disabled="!canPopOut(name)">
          <button
            v-if="popped(name)"
            type="button"
            :class="tabClass(false)"
            :aria-label="`${PAGES[name].title}, in its own window`"
            @click="windows.showWindow(pageWindow(name))"
          >
            <UIcon :name="PAGES[name].icon" class="size-4" />
            <span v-if="!compact">{{ PAGES[name].title }}</span>
            <UIcon name="i-lucide-app-window" class="size-3.5 text-dimmed" />
          </button>
          <div v-else :class="tabClass(route.name === name)">
            <RouterLink
              :to="resolveTabLink(name)"
              :aria-label="PAGES[name].title"
              class="flex h-full items-center gap-1.5"
            >
              <UIcon :name="PAGES[name].icon" class="size-4" />
              <span v-if="!compact || route.name === name">{{ PAGES[name].title }}</span>
            </RouterLink>
            <UTooltip v-if="route.name === name && canPopOut(name)" text="Open in a new window">
              <UButton
                icon="i-lucide-app-window"
                variant="ghost"
                size="xs"
                square
                aria-label="Open in a new window"
                :ui="{ leadingIcon: 'size-3.5' }"
                @click="popOut(name)"
              />
            </UTooltip>
          </div>
        </UContextMenu>
      </nav>
      <span :class="CAPTION" class="min-w-12 grow" @dblclick="Window.ToggleMaximise()" />
    </template>
    <span
      v-else
      :class="CAPTION"
      class="flex min-w-12 grow items-center gap-2 truncate text-13"
      @dblclick="Window.ToggleMaximise()"
    >
      <span class="font-medium text-highlighted">{{ describeWindow(THIS_WINDOW) }}</span>
      <span class="truncate text-dimmed">{{ active?.record.name }}</span>
    </span>

    <UTooltip v-if="IS_MAIN && active" text="Agent pane" :kbds="findKbds('agent.togglePane')">
      <UChip :show="agent.sessions.some(agent.isWaiting)" color="warning" inset size="sm">
        <UButton
          icon="i-lucide-sparkles"
          :variant="agent.isPaneOpen ? 'soft' : 'ghost'"
          size="sm"
          square
          aria-label="Agent pane"
          @click="agent.togglePane()"
        />
      </UChip>
    </UTooltip>
    <UTooltip text="Search and commands" :kbds="findKbds('palette.open')">
      <UButton
        icon="i-lucide-search"
        variant="ghost"
        size="sm"
        square
        aria-label="Search and commands"
        @click="commandBus.emit('palette.open')"
      />
    </UTooltip>
    <div class="-my-px flex shrink-0 self-stretch" :class="ROUND && 'items-center gap-2.5 pe-2.5'">
      <button
        v-for="button in windowButtons"
        :key="button.region"
        type="button"
        :aria-label="button.label"
        :class="styleButton(button.region)"
        :style="{ '--wails-non-client-region': button.region }"
        @click="button.press()"
      >
        <svg
          viewBox="0 0 10 10"
          class="fill-none stroke-current"
          :class="ROUND ? 'size-2 stroke-[1.5]' : 'size-2.5 stroke-1'"
          aria-hidden="true"
        >
          <path :d="button.glyph" />
        </svg>
      </button>
    </div>
  </header>
</template>
