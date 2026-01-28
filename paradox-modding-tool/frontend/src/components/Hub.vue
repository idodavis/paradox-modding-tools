<template>
  <div class="flex-1 flex flex-col min-h-0 min-w-0 p-4 sm:p-6 overflow-auto">
    <div class="max-w-2xl mx-auto w-full pt-8 sm:pt-12">
      <p class="text-center mb-8 sm:mb-10">
        Tools for comparing and merging Paradox game script files
      </p>
      <div class="grid gap-4 sm:gap-6 sm:grid-cols-2">
        <Button @click="$emit('select', 'comparison')" class="flex flex-col w-full p-6">
          <span class="text-lg font-semibold mb-2">File comparison & diff</span>
          <span class="text-sm">Compare two file sets or directories and view diffs side-by-side or
            unified.</span>
        </Button>
        <Button @click="$emit('select', 'merge')" class="flex flex-col w-full p-6">
          <span class="text-lg font-semibold mb-2">Script merger</span>
          <span class="text-sm">Merge Paradox script files (base + mod) with configurable options.</span>
        </Button>
      </div>
    </div>
  </div>
</template>

<script>
import Button from 'primevue/button'

export default {
  name: 'Hub',
  components: { Button },
  emits: ['select']
}
</script>
