<script setup lang="ts">
/**
 * Vue app root with library-centric shell and workbench singleton host.
 */
import { computed, onMounted, ref, shallowRef, watch } from "vue";
import { useColorMode } from "@vueuse/core";
import { useRoute, useRouter } from "vue-router";
import type { DropdownMenuItem } from "@nuxt/ui";
import appIcon from "@assets/PMT-SquareIcon-Mint.png?url";
import PMTLogo from "./components/PMTLogo.vue";
import GameIcon from "./components/GameIcon.vue";
import { useWorkspaceStore } from "./stores/workspace";
import { useSettingsStore } from "./stores/settings";
import {
  applySeedCss,
  familyLabel,
  normalizeThemeFamily,
  themeMenuItems,
  workbenchThemeId,
  type PmtThemeFamily,
  type ThemeMenuItem,
  type ThemeSwatchColors,
} from "./ide/colorThemes";
import { CheckForUpdates, GetVersion } from "@services/configservice";
import { CopyDiagnostics, LastRunCrashed } from "@services/logservice";
import { isWorkbenchReady } from "./ide/workbenchHost";
import IdeWorkbenchLayout from "./components/IdeWorkbenchLayout.vue";
import IdeStatusStrip from "./components/IdeStatusStrip.vue";
import { applyEditorFontSize, applyWorkbenchTheme } from "./ide/themeBridge";
import DisplayPopover from "./components/DisplayPopover.vue";
import { workspaceHomeRoute } from "./workspaceTools";

const route = useRoute();
const router = useRouter();
const ws = useWorkspaceStore();
const settings = useSettingsStore();
const colorMode = useColorMode();
const currentFamily = shallowRef<PmtThemeFamily>("pmt");
const appearance = computed(() =>
  colorMode.value === "dark" ? ("dark" as const) : ("light" as const),
);
const themeItems = computed(() => themeMenuItems(appearance.value));
const currentThemeSwatches = computed(
  (): ThemeSwatchColors =>
    themeItems.value.find((i) => i.value === currentFamily.value)?.swatches ??
    themeItems.value[0]!.swatches,
);
const currentFamilyLabel = computed(() => familyLabel(currentFamily.value));
const version = ref("...");
const helpOpen = ref(false);

const currentTitle = computed(() => route.meta.title ?? "Tools");
const currentDescription = computed(() => route.meta.description ?? "");
const helpParagraphs = computed(() => route.meta.help ?? []);
const isLibrary = computed(() => route.name === "library");
const showWorkbench = computed(
  () => route.name === "workspace-ide",
);

const headerItems = computed(() => [
  { label: "Library", icon: "i-lucide-library", to: { name: "library" } },
  { label: currentTitle.value },
]);

/** Game id attached to a workspace dropdown group label. */
function menuGameId(item: unknown): string {
  if (!item || typeof item !== "object" || !("gameId" in item)) return "";
  const id = (item as { gameId?: unknown }).gameId;
  return typeof id === "string" ? id : "";
}

const workspaceDropdownItems = computed<DropdownMenuItem[][]>(() => {
  const items: DropdownMenuItem[][] = [];
  const grouped = ws.workspacesByGame;
  const activeId = ws.activeWorkspaceId;
  const activeGame = ws.activeWorkspace?.gameId ?? ws.currentGameId;

  const toItem = (workspace: (typeof ws.workspaces)[number]): DropdownMenuItem => ({
    label: workspace.name,
    icon: workspace.id === activeId ? "i-lucide-check" : "i-lucide-folder",
    onSelect: () => {
      ws.setActiveWorkspace(workspace.id);
      void router.push(workspaceHomeRoute(workspace, settings.visibleTools));
    },
  });
  const gameLabel = (gameId: string): DropdownMenuItem => ({
    label: ws.shortName(gameId), type: "label" as const, disabled: true,
    slot: "game-group", gameId,
  });

  const ordered = [
    ...grouped.filter((s) => s.gameId === activeGame),
    ...grouped.filter((s) => s.gameId !== activeGame),
  ];
  for (const section of ordered) {
    const limit = section.gameId === activeGame ? 8 : 5;
    items.push([
      gameLabel(section.gameId),
      ...section.workspaces.slice(0, limit).map(toItem),
    ]);
  }
  items.push([
    ...(activeId && ws.hasWorkspaces
      ? [{
        label: "Configure workspace…",
        icon: "i-lucide-settings",
        onSelect: () => openWorkspaceSettings(),
      } satisfies DropdownMenuItem]
      : []),
    {
      label: "New Workspace", icon: "i-lucide-plus",
      onSelect: () => router.push({ name: "wizard" })
    },
    {
      label: "Library", icon: "i-lucide-library",
      onSelect: () => router.push({ name: "library" })
    },
  ]);
  return items;
});

/** Apply a palette family to `data-theme` (appearance is color-mode). */
function setFamily(family: PmtThemeFamily): void {
  currentFamily.value = family;
  document.documentElement.dataset.theme = family;
  applySeedCss(workbenchThemeId(family, appearance.value));
}

/** Persist the palette family via settings store. */
async function saveFamily(family: PmtThemeFamily): Promise<void> {
  await settings.set("_global.theme", family);
}

/** Load version string. */
async function loadVersion(): Promise<void> {
  try {
    version.value = await GetVersion();
  } catch (error) {
    version.value = error instanceof Error ? error.message : String(error);
  }
}

/** Persist a user-picked palette family; CSS apply is the family/color-mode watch. */
async function onThemeChange(theme: string | null): Promise<void> {
  if (!theme) return;
  const family = normalizeThemeFamily(theme);
  if (family === currentFamily.value) return;
  setFamily(family);
  await saveFamily(family);
}

/** Swatch colors for a SelectMenu theme item. */
function itemSwatches(
  item: ThemeMenuItem | string | undefined,
): ThemeSwatchColors {
  if (!item) return themeItems.value[0]!.swatches;
  if (typeof item === "string") {
    return (
      themeItems.value.find((i) => i.value === normalizeThemeFamily(item))
        ?.swatches ?? themeItems.value[0]!.swatches
    );
  }
  return item.swatches;
}

/** Open workspace settings for the active workspace, or Library if none. */
function openWorkspaceSettings(): void {
  const id = ws.activeWorkspaceId;
  if (id) {
    void router.push({ name: "workspace-settings", params: { id } });
    return;
  }
  void router.push({ name: "library" });
}

/** Check for app updates. */
async function checkForUpdates(): Promise<void> {
  await CheckForUpdates();
}

const toast = useToast();

/** Put the report for a bug report on the clipboard. */
async function copyDiagnostics(): Promise<void> {
  try {
    await CopyDiagnostics();
    toast.add({ title: "Diagnostics copied", color: "success" });
  } catch (e) {
    toast.add({ title: e instanceof Error ? e.message : String(e), color: "error" });
  }
}

/** Tell of a last run that ended without closing: a crash, or the app ended from outside. */
async function noticeCrash(): Promise<void> {
  if (!(await LastRunCrashed())) return;
  toast.add({
    title: "PMT closed unexpectedly",
    description: "The last session ended without closing. Copy diagnostics and paste them into a bug report on GitHub.",
    color: "warning",
    icon: "i-lucide-triangle-alert",
    duration: 0,
    actions: [
      {
        label: "Copy diagnostics",
        color: "warning",
        onClick: (e?: Event) => {
          e?.stopPropagation();
          void copyDiagnostics();
        },
      },
    ],
  });
}

watch(
  [currentFamily, () => colorMode.value],
  async () => {
    document.documentElement.dataset.theme = currentFamily.value;
    applySeedCss(workbenchThemeId(currentFamily.value, appearance.value));
    if (isWorkbenchReady()) {
      await applyWorkbenchTheme(
        workbenchThemeId(currentFamily.value, appearance.value),
      );
    }
  },
);

onMounted(async () => {
  await Promise.all([settings.load(), loadVersion(), ws.refresh()]);
  applyEditorFontSize(settings.editorFontSize);
  const stored = settings.values["_global.theme"];
  const family = normalizeThemeFamily(stored);
  if (colorMode.store.value === "auto") {
    colorMode.store.value = "dark";
  }
  setFamily(family);
  const link = document.querySelector<HTMLLinkElement>('link[rel="icon"]');
  if (link) link.href = appIcon;
  void noticeCrash();
});
</script>

<template>
  <UApp>
    <div class="pmt-root flex h-screen min-h-0 flex-col overflow-hidden">
      <header class="z-10 shrink-0 border-b border-default bg-default px-2 py-1 shadow-sm">
        <div class="flex flex-wrap items-center justify-between gap-2">
          <div class="flex min-w-0 flex-1 items-center gap-3">
            <PMTLogo :icon-height="40" :text-height="48" class="-ml-4.5" />
            <UBreadcrumb v-if="!isLibrary" :items="headerItems" />
          </div>
          <div class="flex items-center gap-2">
            <UDropdownMenu :items="workspaceDropdownItems">
              <template #game-group-leading="{ item }">
                <GameIcon :game-id="menuGameId(item)" />
              </template>
              <UButton :label="ws.activeWorkspaceName" trailing-icon="i-lucide-chevron-down" color="neutral"
                variant="outline" class="max-w-48 truncate">
                <template v-if="ws.activeWorkspace?.gameId" #leading>
                  <GameIcon :game-id="ws.activeWorkspace.gameId" />
                </template>
              </UButton>
            </UDropdownMenu>
            <USelectMenu :model-value="currentFamily" :items="themeItems" value-key="value" :ui="{
              content: 'min-w-44',
              base: 'w-auto gap-1.5 ps-2 pe-2',
              leading: 'static inset-auto',
              trailing: 'static inset-auto',
            }" @update:model-value="onThemeChange">
              <template #leading>
                <span class="inline-flex shrink-0 overflow-hidden rounded-sm border border-default" aria-hidden="true">
                  <span v-for="(color, i) in currentThemeSwatches" :key="i" class="size-3.5"
                    :style="{ backgroundColor: color }" />
                </span>
              </template>
              <template #default>
                <span class="text-sm">{{ currentFamilyLabel }}</span>
              </template>
              <template #item-leading="{ item }">
                <span class="inline-flex shrink-0 overflow-hidden rounded-sm border border-default" aria-hidden="true">
                  <span v-for="(color, i) in itemSwatches(item as ThemeMenuItem)" :key="i" class="size-3.5"
                    :style="{ backgroundColor: color }" />
                </span>
              </template>
            </USelectMenu>
            <UColorModeButton />
            <DisplayPopover />
            <UTooltip v-if="ws.hasWorkspaces" text="Workspace settings">
              <UButton icon="i-lucide-settings" color="neutral" variant="ghost" @click="openWorkspaceSettings" />
            </UTooltip>
          </div>
        </div>
      </header>

      <main class="relative flex min-h-0 flex-1 flex-col overflow-hidden">
        <IdeWorkbenchLayout :visible="showWorkbench" :theme="currentFamily">
          <template #toolbar>
            <div class="shrink-0 border-b border-default" :class="{ hidden: route.name !== 'workspace-ide' }">
              <router-view name="ide" v-slot="{ Component }">
                <component
                  :is="Component"
                  v-if="Component"
                  :key="String(route.params.id ?? '')"
                />
              </router-view>
            </div>
          </template>
        </IdeWorkbenchLayout>
        <div class="absolute inset-0 overflow-hidden bg-default"
          :class="showWorkbench ? 'z-0 pointer-events-none' : 'z-20'">
          <router-view v-slot="{ Component }">
            <KeepAlive :max="10">
              <component
                :is="Component"
                v-if="Component && route.meta.keepAlive"
                :key="String(route.name)"
              />
            </KeepAlive>
            <component
              :is="Component"
              v-if="Component && !route.meta.keepAlive"
              :key="String(route.name)"
            />
          </router-view>
        </div>
      </main>

      <footer class="relative z-10 flex shrink-0 items-center justify-between
          border-t border-default bg-default px-2 text-sm text-default">
        <div class="relative z-10 min-w-0">
          <IdeStatusStrip v-if="showWorkbench" />
        </div>
        <div class="pointer-events-none absolute inset-0 flex items-center justify-center">
          <PMTLogo :icon-height="25" :text-height="30" />
        </div>
        <div class="relative z-10 flex items-center gap-2">
          <UButton :label="`Help for ${currentTitle}`" icon="i-lucide-circle-help" color="neutral" variant="ghost"
            size="sm" @click="helpOpen = true" />
          <UButton :label="version" icon="i-lucide-refresh-cw" color="neutral" variant="ghost" size="sm"
            @click="checkForUpdates" />
        </div>
      </footer>

      <UModal v-model:open="helpOpen" :title="currentTitle" :description="currentDescription"
        :ui="{ content: 'sm:max-w-2xl' }">
        <template #body>
          <div class="space-y-3 text-sm text-muted">
            <p v-for="(para, i) in helpParagraphs" :key="i">{{ para }}</p>
            <p v-if="!helpParagraphs.length">{{ currentDescription }}</p>
          </div>
        </template>
        <template #footer="{ close }">
          <UButton label="Close" color="neutral" variant="outline" icon="i-lucide-x" @click="close" />
        </template>
      </UModal>
    </div>
  </UApp>
</template>
