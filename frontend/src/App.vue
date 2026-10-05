<script setup lang="ts">
/**
 * The app's frame: the top bar, the page over the IDE's workbench (kept mounted behind every page), and
 * the status bar. The pages are kept alive, so leaving one for another keeps its state.
 */
import { computed, onMounted } from "vue";
import { useRoute } from "vue-router";
import appIcon from "@assets/PMT-SquareIcon-Mint.png?url";

const route = useRoute();
const onIde = computed(() => route.name === "workspace-ide");
const KEPT = ["LibraryPage", "EventGraphPage", "HealthPage", "PatcherPage", "ReleasePage", "WorkspaceSettingsPage"];

onMounted(() => {
  const link = document.querySelector<HTMLLinkElement>('link[rel="icon"]');
  if (link) link.href = appIcon;
});
</script>

<template>
  <UApp>
    <div class="flex h-screen min-h-0 flex-col overflow-hidden bg-default">
      <TopBar />
      <main class="relative min-h-0 flex-1 overflow-hidden">
        <IdeWorkbenchLayout :visible="onIde">
          <template #toolbar>
            <RouterView name="ide" />
          </template>
        </IdeWorkbenchLayout>
        <div class="absolute inset-0 overflow-hidden bg-default" :class="onIde ? 'pointer-events-none z-0' : 'z-20'">
          <RouterView v-slot="{ Component, route: shown }">
            <KeepAlive :include="KEPT">
              <component :is="Component" :key="shown.name" />
            </KeepAlive>
          </RouterView>
        </div>
      </main>
      <StatusBar />
    </div>
  </UApp>
</template>
