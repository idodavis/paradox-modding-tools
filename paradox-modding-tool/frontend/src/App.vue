<template>
  <div class="flex flex-col h-full min-h-0">
    <header
      class="relative px-6 py-4 bg-(--p-surface-800) border-b border-(--p-surface-700) flex items-center justify-between gap-4">
      <div
        class="absolute inset-x-0 top-0 h-0.5 bg-linear-to-r from-transparent via-(--p-primary-500)/50 to-transparent">
      </div>
      <a class="group flex items-center gap-4 w-fit cursor-pointer transition-transform hover:opacity-90"
        @click="currentPage = 'hub'">
        <div
          class="flex items-center justify-center w-14 h-14 bg-(--p-surface-900) rounded-xl border border-(--p-surface-700) transition-all group-hover:border-(--p-primary-500)/50">
          <Icon icon="logos:adonisjs-icon" class="w-8 h-8 transition-transform group-hover:scale-105" />
        </div>
        <div class="flex flex-col items-start">
          <h1 class="text-2xl font-bold text-(--p-surface-100) tracking-tight">
            Paradox Modding Tool</h1>
          <span class="text-xs text-(--p-surface-400) font-medium tracking-wider uppercase">Tools and Utilities for
            Paradox Mod
            Developers</span>
        </div>
      </a>
      <div class="flex items-center gap-2 ml-auto">
        <Select v-model="game" :options="gameOptions" option-label="label" option-value="value" placeholder="Game"
          class="w-32" @update:model-value="onGameChange" />
        <Button icon="pi pi-cog" rounded text severity="secondary" aria-label="Settings"
          @click="currentPage = 'settings'" />
        <Button icon="pi pi-question-circle" rounded text severity="secondary" aria-label="Help"
          @click="helpDrawerVisible = true" />
      </div>
    </header>

    <div class="flex flex-col flex-1 min-h-0 overflow-auto">
      <div v-if="currentPage === 'hub'" class="flex-1 flex flex-col min-h-0">
        <Hub @select="currentPage = $event" />
      </div>
      <div v-else-if="currentPage === 'comparison'" class="flex-1 flex flex-col min-h-0">
        <ComparisonTool />
      </div>
      <div v-else-if="currentPage === 'merge'" class="flex-1 flex flex-col min-h-0">
        <MergeTool />
      </div>
      <div v-else-if="currentPage === 'inventory'" class="flex-1 flex flex-col min-h-0">
        <InventoryTool />
      </div>
      <div v-else-if="currentPage === 'modding-docs'" class="flex-1 flex flex-col min-h-0">
        <ModdingDocs />
      </div>
      <div v-else-if="currentPage === 'settings'" class="flex-1 flex flex-col min-h-0">
        <Settings />
      </div>
    </div>

    <footer
      class="shrink-0 h-6 px-3 flex items-center justify-end border-t border-(--p-surface-700) bg-(--p-surface-900)">
      <span class="text-xs text-(--p-surface-500)">v{{ appVersion }}</span>
    </footer>

    <Drawer v-model:visible="helpDrawerVisible" position="right" :header="helpContent.title"
      :style="{ width: 'min(400px, 100vw)' }">
      <p class="text-sm text-(--p-surface-300)">{{ helpContent.body }}</p>
    </Drawer>
  </div>
</template>

<script setup>
import { ref, computed } from 'vue'
import Hub from './pages/Hub.vue'

/** App version (semver). Single source of truth for footer. */
const appVersion = '0.5.0'
import ComparisonTool from './pages/ComparisonTool.vue'
import MergeTool from './pages/MergeTool.vue'
import InventoryTool from './pages/InventoryTool.vue'
import ModdingDocs from './pages/ModdingDocs.vue'
import Settings from './pages/Settings.vue'
import { Icon } from '@iconify/vue'
import { useAppSettings } from './composables/useAppSettings'
import { getHelpForPage } from './utils/helpContent'

const { game, setGame } = useAppSettings()
const currentPage = ref('hub')
const helpDrawerVisible = ref(false)

const gameOptions = [
  { label: 'CK3', value: 'ck3' },
  { label: 'EU5', value: 'eu5' }
]

function onGameChange(value) {
  if (value === 'ck3' || value === 'eu5') setGame(value)
}

const helpContent = computed(() => getHelpForPage(currentPage.value))
</script>
