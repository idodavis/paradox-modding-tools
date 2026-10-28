<!--
  The app's frame: the top bar, the page with the agent pane beside it in the main window, or the pane alone in a
  window of its own, the status bar, and the palette and Settings over them.
-->
<script setup lang="ts">
import type { SplitterItem } from "@nuxt/ui";
import { computed } from "vue";
import { useRoute } from "vue-router";

import { useAgentStore } from "../stores/agent";
import { THIS_WINDOW } from "../stores/windows";
import AgentPane from "./AgentPane.vue";
import { registerShortcuts } from "./commands";
import CommandPalette from "./CommandPalette.vue";
import { isWorkspacePage } from "./pages";
import { KEPT } from "./router";
import SettingsModal from "./settings/SettingsModal.vue";
import StatusBar from "./StatusBar.vue";
import TopBar from "./TopBar.vue";

registerShortcuts();
const route = useRoute();
const agent = useAgentStore();

// the pane sits beside a workspace's pages in the main window
const panes = computed<SplitterItem[]>(() => [
  { id: "page", slot: "page", order: 1, minSize: 40 },
  ...(THIS_WINDOW === "main" && agent.isPaneOpen && route.name && isWorkspacePage(route.name)
    ? [{ id: "agent", slot: "agent", order: 2, sizeUnit: "px" as const, defaultSize: 400, minSize: 320 }]
    : []),
]);
</script>

<template>
  <UApp>
    <div class="flex h-screen min-h-0 flex-col overflow-hidden bg-default">
      <TopBar />
      <main class="flex min-h-0 flex-1 overflow-hidden">
        <AgentPane v-if="THIS_WINDOW === 'agent'" />
        <USplitter v-else :items="panes" auto-save-id="pmt.agent.pane" class="min-w-0 flex-1">
          <template #page>
            <div class="relative min-h-0 min-w-0 flex-1 overflow-hidden">
              <RouterView v-slot="{ Component, route: current }">
                <KeepAlive :include="[...KEPT]">
                  <component :is="Component" :key="current.name" />
                </KeepAlive>
              </RouterView>
            </div>
          </template>
          <template #agent>
            <AgentPane />
          </template>
        </USplitter>
      </main>
      <StatusBar />
      <CommandPalette />
      <SettingsModal />
    </div>
  </UApp>
</template>
