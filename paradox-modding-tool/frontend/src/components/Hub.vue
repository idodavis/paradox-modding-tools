<template>
  <div class="flex-1 flex flex-col min-h-0 min-w-0 p-4 sm:p-6 overflow-auto bg-dark-input">
    <div class="max-w-2xl mx-auto w-full pt-8 sm:pt-12">
      <p class="text-gray-400 text-center mb-8 sm:mb-10">
        Tools for comparing and merging Paradox game script files
      </p>
      <div class="grid gap-4 sm:gap-6 sm:grid-cols-2">
        <button
          @click="$emit('select', 'comparison')"
          class="flex flex-col p-6 rounded-xl bg-dark-panel/60 backdrop-blur-sm border border-dark-border/50 shadow-material text-left hover:bg-dark-panel/80 hover:border-dark-border transition-all duration-200 focus:outline-none focus:ring-2 focus:ring-btn-primary/50"
        >
          <span class="text-lg font-semibold text-gray-200 mb-2">File comparison & diff</span>
          <span class="text-sm text-gray-400">Compare two file sets or directories and view diffs side-by-side or unified.</span>
        </button>
        <button
          @click="$emit('select', 'merge')"
          class="flex flex-col p-6 rounded-xl bg-dark-panel/60 backdrop-blur-sm border border-dark-border/50 shadow-material text-left hover:bg-dark-panel/80 hover:border-dark-border transition-all duration-200 focus:outline-none focus:ring-2 focus:ring-btn-primary/50"
        >
          <span class="text-lg font-semibold text-gray-200 mb-2">Script merger</span>
          <span class="text-sm text-gray-400">Merge Paradox script files (base + mod) with configurable options.</span>
        </button>
      </div>
    </div>
  </div>
</template>

<script>
export default {
  name: 'Hub',
  emits: ['select']
}
</script>
