<script setup lang="ts">
/**
 * The app's frame: the top bar (the main window's only; another window shows one thing), the page, and the status
 * bar, and the command palette in every window. The pages are kept alive, so leaving one for another keeps its state
 * (the IDE's editors and drafts among it).
 */
import { openPalette } from "./components/app/CommandPalette.vue";
import { isMain } from "./windows";

// with a modifier, so they work while typing too (the editor's text is an input to Nuxt UI)
defineShortcuts({
  meta_k: { handler: () => openPalette(), usingInput: true },
  meta_shift_p: { handler: () => openPalette(), usingInput: true },
  meta_p: { handler: () => openPalette(""), usingInput: true },
});

const KEPT = [
  "LibraryPage",
  "WorkspaceIdePage",
  "EventGraphPage",
  "HealthPage",
  "PatchNotesPage",
  "ReleasePage",
  "WorkspaceSettingsPage",
];
</script>

<template>
  <UApp>
    <div class="flex h-screen min-h-0 flex-col overflow-hidden bg-default">
      <TopBar v-if="isMain" />
      <main class="relative min-h-0 flex-1 overflow-hidden">
        <RouterView v-slot="{ Component, route: shown }">
          <KeepAlive :include="KEPT">
            <component :is="Component" :key="shown.name" />
          </KeepAlive>
        </RouterView>
      </main>
      <StatusBar />
      <CommandPalette />
    </div>
  </UApp>
</template>
