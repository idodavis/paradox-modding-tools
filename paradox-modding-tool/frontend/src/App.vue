<template>
  <div class="app-theme flex flex-col w-full h-full min-h-0 bg-dark-bg text-gray-200">
    <header
      class="shrink-0 bg-dark-panel/80 backdrop-blur-md px-4 sm:px-8 py-3 sm:py-4 border-b border-primary-600 flex justify-between items-center z-50 relative">
      <div class="flex items-center gap-3 min-w-0">
        <Button v-if="currentPage !== 'hub'" label="Home" @click="currentPage = 'hub'"
          class="btn-secondary text-sm py-2 px-3 shrink-0" />
        <h1 class="text-lg sm:text-2xl font-semibold tracking-tight truncate min-w-0">Paradox Modding Tool</h1>
      </div>
    </header>

    <div v-show="currentPage === 'hub'" class="flex-1 min-h-0 min-w-0 flex flex-col overflow-hidden">
      <Hub @select="currentPage = $event" />
    </div>
    <div v-show="currentPage === 'comparison'" class="flex-1 min-h-0 min-w-0 flex flex-col overflow-hidden">
      <ComparisonTool />
    </div>
    <div v-show="currentPage === 'merge'" class="flex-1 min-h-0 min-w-0 flex flex-col overflow-hidden">
      <MergeTool />
    </div>
  </div>
</template>

<script>
import Button from 'primevue/button'
import Hub from './components/Hub.vue'
import ComparisonTool from './components/ComparisonTool.vue'
import MergeTool from './components/MergeTool.vue'

export default {
  name: 'App',
  components: {
    Button,
    Hub,
    ComparisonTool,
    MergeTool
  },
  data() {
    return {
      currentPage: 'hub'
    }
  }
}
</script>
