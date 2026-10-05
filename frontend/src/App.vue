<script setup lang="ts">
/**
 * Vue app root with library-centric shell and workbench singleton host.
 */
import { computed, onMounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import type { DropdownMenuItem } from "@nuxt/ui";
import appIcon from "@assets/PMT-SquareIcon-Mint.png?url";
import PMTLogo from "./components/PMTLogo.vue";
import GameIcon from "./components/GameIcon.vue";
import { useWorkspaceStore } from "./stores/workspace";
import { useSettingsStore } from "./stores/settings";
import { CheckForUpdates, GetVersion } from "@services/appservice";
import { CopyDiagnostics, LastRunCrashed } from "@services/logservice";
import IdeWorkbenchLayout from "./components/IdeWorkbenchLayout.vue";
import IdeStatusStrip from "./components/IdeStatusStrip.vue";
import { workspaceHomeRoute } from "./workspaceTools";

const route = useRoute();
const router = useRouter();
const ws = useWorkspaceStore();
const settings = useSettingsStore();
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

/** Load version string. */
async function loadVersion(): Promise<void> {
  try {
    version.value = await GetVersion();
  } catch (error) {
    version.value = error instanceof Error ? error.message : String(error);
  }
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

onMounted(async () => {
  await Promise.all([loadVersion(), ws.refresh()]);
  const link = document.querySelector<HTMLLinkElement>('link[rel="icon"]');
  if (link) link.href = appIcon;
  void noticeCrash();
});
</script>

<template>
  <UApp>
    <div class="pmt-root flex h-screen min-h-0 flex-col overflow-hidden">
      <header class="z-10 shrink-0 border-b border-default bg-default px-2 py-1">
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
            <UTooltip :text="settings.mode === 'dark' ? 'Switch to light' : 'Switch to dark'">
              <UButton
                :icon="settings.mode === 'dark' ? 'i-lucide-sun' : 'i-lucide-moon'"
                variant="ghost"
                square
                :aria-label="settings.mode === 'dark' ? 'Switch to light' : 'Switch to dark'"
                @click="settings.toggleMode()"
              />
            </UTooltip>
            <UTooltip v-if="ws.hasWorkspaces" text="Workspace settings">
              <UButton icon="i-lucide-settings" color="neutral" variant="ghost" @click="openWorkspaceSettings" />
            </UTooltip>
          </div>
        </div>
      </header>

      <main class="relative flex min-h-0 flex-1 flex-col overflow-hidden">
        <IdeWorkbenchLayout :visible="showWorkbench">
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
