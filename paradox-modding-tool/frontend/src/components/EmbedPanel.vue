<template>
  <div
    class="flex flex-col flex-1 min-h-0 rounded-lg border border-(--p-surface-600) dark:border-(--p-surface-700) overflow-hidden">
    <div
      class="shrink-0 flex items-center justify-between gap-2 py-2 px-4 border-b border-(--p-surface-600) dark:border-(--p-surface-700)">
      <span class="text-sm font-medium">{{ title }}</span>
      <Button label="Open in browser" icon="pi pi-external-link" severity="secondary" size="small"
        @click="openURLInBrowser" />
    </div>
    <iframe v-if="url" :src="url" :title="title" class="w-full flex-1 min-h-0 border-0 block" />
  </div>
</template>

<script setup>
import Button from 'primevue/button'
import { openURLInBrowser } from '../utils/general'

defineProps({
  url: { type: String, default: '' },
  title: { type: String, default: 'Embed' }
})

</script>
