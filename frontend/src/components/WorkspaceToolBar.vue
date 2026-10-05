<script setup lang="ts">
/**
 * Shared toolbar for workspace tool routes (IDE, graph, health, patcher, release). It boots the
 * workbench behind whichever of them opens first, so the IDE and every code snippet are ready.
 */
import { computed } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useQuery } from "@pinia/colada";
import type { NavigationMenuItem } from "@nuxt/ui";
import { GetIdeRoots } from "@services/workspaceservice";
import { EnsureSession } from "@services/sessionservice";
import { pageTheme } from "../ide/colorThemes";
import { setWorkbenchRoots } from "../ide/workbenchHost";
import { useSettingsStore } from "../stores/settings";
import { useWorkspaceStore } from "../stores/workspace";
import { WORKSPACE_TOOLS } from "../workspaceTools";

const props = defineProps<{
  workspaceId: string;
}>();

const route = useRoute();
const router = useRouter();
const settings = useSettingsStore();
const ws = useWorkspaceStore();

const id = computed(() => props.workspaceId || String(route.params.id ?? ""));
const title = computed(() => route.meta.title ?? "");
const onIde = computed(() => route.name === "workspace-ide");

// the folders need no session, so they mount first and a switch shows the new workspace at once
const { error, isPending } = useQuery({
  key: () => ["ide-boot", id.value],
  query: async () => {
    await ws.loadActiveWorkspace();
    const rec = await GetIdeRoots(id.value);
    const roots = rec?.roots ?? [];
    if (!roots.length) {
      throw new Error("No game or mod paths available for this workspace.");
    }
    await setWorkbenchRoots(roots, pageTheme(), {
      workspaceId: id.value,
      persistTabs: !rec?.resetIdeOnOpen,
      openFiles: rec?.ideOpenFiles ?? [],
      activeFile: rec?.ideActiveFile ?? "",
      hideExplorerBinaries: rec?.hideExplorerBinaries !== false,
    });
    await EnsureSession(id.value);
    return true;
  },
  enabled: () => !!id.value,
});

/** History back unless the previous entry is the create-workspace wizard. */
function goBack(): void {
  const back = window.history.state?.back;
  if (!back || String(back).includes("/wizard")) {
    void router.replace({ name: "library" });
    return;
  }
  router.back();
}

/** Tool routes as navigation-menu items with active-state highlighting. */
const toolItems = computed<NavigationMenuItem[]>(() => {
  return WORKSPACE_TOOLS.filter((t) => settings.visibleTools.includes(t.name)).map((t) => ({
    label: t.label,
    icon: t.icon,
    active: route.name === t.name,
    to: { name: t.name, params: { id: id.value } },
  }));
});
</script>

<template>
  <UDashboardToolbar class="px-2 sm:px-2">
    <template #left>
      <UButton
        icon="i-lucide-arrow-left"
        label="Back"
        color="neutral"
        variant="ghost"
        size="sm"
        @click="goBack"
      />
      <span class="font-semibold text-default">{{ title }}</span>
      <UNavigationMenu
        :items="toolItems"
        variant="pill"
        highlight
        class="flex-wrap"
      />
    </template>
    <template #right>
      <span v-if="onIde && isPending" class="text-xs text-muted">Loading workbench…</span>
      <span v-if="onIde && error" class="text-xs text-error">{{ error.message }}</span>
      <slot name="trailing" />
    </template>
  </UDashboardToolbar>
</template>
