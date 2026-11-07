<!-- The app's frame: the top bar, the page with the agent pane, the status bar, and the palette and Settings. -->
<script setup lang="ts">
import type { SplitterItem } from "@nuxt/ui";
import { computed, useTemplateRef } from "vue";
import { useRoute } from "vue-router";

import { useSavedPixelSize } from "../lib/splitter";
import { useAgentStore } from "../stores/agent";
import { THIS_WINDOW } from "../stores/windows";
import { useWorkspaceStore } from "../stores/workspace";
import AgentPane from "./AgentPane.vue";
import { registerShortcuts } from "./commands";
import CommandPalette from "./CommandPalette.vue";
import { isWorkspacePage } from "./pages";
import SettingsModal from "./settings/SettingsModal.vue";
import StatusBar from "./StatusBar.vue";
import TopBar from "./TopBar.vue";

registerShortcuts();
const route = useRoute();
const agent = useAgentStore();
const workspace = useWorkspaceStore();

const agentWidth = useSavedPixelSize(useTemplateRef("splitter"), 1, "pmt.agent.width", 400);
// the pane sits beside a workspace's pages in the main window
const panes = computed<SplitterItem[]>(() => [
  { id: "page", slot: "page", order: 1, minSize: 40 },
  ...(THIS_WINDOW === "main" && agent.isPaneOpen && route.name && isWorkspacePage(route.name)
    ? [{ id: "agent", slot: "agent", order: 2, sizeUnit: "px" as const, defaultSize: agentWidth.size(), minSize: 320 }]
    : []),
]);
</script>

<template>
  <UApp>
    <div class="flex h-screen min-h-0 flex-col overflow-hidden bg-default">
      <TopBar />
      <main class="flex min-h-0 flex-1 overflow-hidden">
        <AgentPane v-if="THIS_WINDOW === 'agent'" />
        <USplitter
          v-else
          ref="splitter"
          :items="panes"
          class="min-w-0 flex-1"
          @layout="panes.length === 2 && agentWidth.keep($event)"
        >
          <template #page>
            <div class="relative min-h-0 min-w-0 flex-1 overflow-hidden">
              <RouterView v-slot="{ Component, route: current }">
                <!-- a workspace switch drops the kept pages, so none asks the new workspace for the old one's names -->
                <KeepAlive :key="workspace.activeId()" exclude="WizardPage">
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
