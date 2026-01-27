<template>
  <div class="flex flex-col w-full h-full min-h-0 bg-dark-bg text-gray-200">
    <header
      class="flex-shrink-0 bg-dark-panel/80 backdrop-blur-md px-4 sm:px-8 py-3 sm:py-4 border-b border-dark-border flex justify-between items-center shadow-material z-50 relative">
      <h1 class="text-lg sm:text-2xl font-semibold tracking-tight truncate min-w-0">Paradox Modding Tool</h1>
    </header>

    <main class="flex-1 min-h-0 min-w-0 flex flex-col overflow-hidden">
      <ComparisonTool />
    </main>
  </div>
</template>

<script>
import ComparisonTool from './components/ComparisonTool.vue'

export default {
  name: 'App',
  components: {
    ComparisonTool
  }
}
</script>
