<!-- The app's frame: the top bar, the page, the status bar, and the palette and Settings over them. -->
<script setup lang="ts">
import { registerShortcuts } from "./commands";
import CommandPalette from "./CommandPalette.vue";
import { KEPT } from "./router";
import SettingsModal from "./settings/SettingsModal.vue";
import StatusBar from "./StatusBar.vue";
import TopBar from "./TopBar.vue";

registerShortcuts();
</script>

<template>
  <UApp>
    <div class="flex h-screen min-h-0 flex-col overflow-hidden bg-default">
      <TopBar />
      <main class="relative min-h-0 flex-1 overflow-hidden">
        <RouterView v-slot="{ Component, route: current }">
          <KeepAlive :include="[...KEPT]">
            <component :is="Component" :key="current.name" />
          </KeepAlive>
        </RouterView>
      </main>
      <StatusBar />
      <CommandPalette />
      <SettingsModal />
    </div>
  </UApp>
</template>
