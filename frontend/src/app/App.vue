<!-- The app's frame: the top bar, the page, and the status bar. -->
<script setup lang="ts">
import { useEventBus } from "@vueuse/core";

import { THIS_WINDOW } from "../stores/windows";
import { registerShortcuts } from "./commands";
import CommandPalette from "./CommandPalette.vue";
import { KEPT } from "./router";
import StatusBar from "./StatusBar.vue";
import TopBar from "./TopBar.vue";

const isMain = THIS_WINDOW === "main";

registerShortcuts();
defineShortcuts({
  meta_shift_p: { handler: () => useEventBus<string>("pmt.command").emit("palette.open"), usingInput: true },
});
</script>

<template>
  <UApp>
    <div class="flex h-screen min-h-0 flex-col overflow-hidden bg-default">
      <TopBar v-if="isMain" />
      <main class="relative min-h-0 flex-1 overflow-hidden">
        <RouterView v-slot="{ Component, route: current }">
          <KeepAlive :include="[...KEPT]">
            <component :is="Component" :key="current.name" />
          </KeepAlive>
        </RouterView>
      </main>
      <StatusBar />
      <CommandPalette />
    </div>
  </UApp>
</template>
