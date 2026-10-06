<script setup lang="ts">
/**
 * The app's frame: the top bar, the page, and the status bar. The pages are kept alive, so leaving one for
 * another keeps its state (the IDE's editors and drafts among it).
 */
const KEPT = ["LibraryPage", "WorkspaceIdePage", "EventGraphPage", "HealthPage", "PatchNotesPage", "ReleasePage", "WorkspaceSettingsPage"];
</script>

<template>
  <UApp>
    <div class="flex h-screen min-h-0 flex-col overflow-hidden bg-default">
      <TopBar />
      <main class="relative min-h-0 flex-1 overflow-hidden">
        <RouterView v-slot="{ Component, route: shown }">
          <KeepAlive :include="KEPT">
            <component :is="Component" :key="shown.name" />
          </KeepAlive>
        </RouterView>
      </main>
      <StatusBar />
    </div>
  </UApp>
</template>
