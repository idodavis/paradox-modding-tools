<template>
  <div class="flex flex-col h-full overflow-hidden">
    <!-- Header (Drawer/Dialog has its own close button) -->
    <div>
      <Tag :value="item.type" severity="success" />
    </div>

    <!-- Content -->
    <div class="flex-1 overflow-auto p-3">
      <!-- Location -->
      <div class="mb-4">
        <span class="text-sm font-medium text-gray-400 block mb-1">Location</span>
        <p class="text-sm font-mono break-all">{{ item.filePath }}</p>
        <p class="text-xs text-gray-500">Lines {{ item.lineStart }} - {{ item.lineEnd }}</p>
      </div>

      <!-- Quick Actions -->
      <div class="mb-4">
        <span class="text-sm font-medium text-gray-400 block mb-2">Quick Actions</span>
        <div class="flex flex-wrap gap-1">
          <Button label="Copy Key" size="small" severity="secondary" @click="copyToClipboard(item.key)" />
          <Button label="Copy Path" size="small" severity="secondary" @click="copyToClipboard(item.filePath)" />
          <Button label="View Graph" size="small" severity="secondary" @click="emit('view-in-graph', item)" />
        </div>
      </div>

      <!-- References: PrimeVue Panel (toggleable); content constrained and scrollable -->
      <Panel v-if="item.references && item.references.length > 0" :header="`References (${item.references.length})`"
        toggleable class="mb-4">
        <div class="flex flex-col gap-2 min-h-0 overflow-auto max-h-56">
          <div v-for="(ref, idx) in item.references" :key="idx"
            class="flex items-start justify-between gap-2 p-2 rounded text-sm font-mono bg-dark-border/30">
            <div class="min-w-0 flex-1 wrap-break-word">
              <span class="text-blue-400">{{ ref.targetKey }}</span>
              <span class="text-gray-500"> ({{ ref.targetType }})</span>
              <span class="text-gray-600 block text-xs break-all">{{ ref.context }} @ {{ ref.sourceFile }}:{{
                ref.sourceLine }}</span>
            </div>
            <Button icon="pi pi-copy" size="small" severity="secondary" outlined aria-label="Copy key"
              @click.stop="copyToClipboard(ref.targetKey)" />
          </div>
        </div>
      </Panel>

      <!-- Raw Text: PrimeVue Panel (toggleable); content constrained and scrollable -->
      <Panel header="Raw Text" toggleable class="mb-4">
        <div class="flex flex-col gap-2 min-h-0 overflow-auto max-h-72">
          <Button icon="pi pi-copy" size="medium" severity="secondary" outlined
            @click="copyToClipboard(item.rawText)" />
          <pre
            class="text-xs font-mono p-2 rounded overflow-auto flex-1 whitespace-pre-wrap break-all bg-dark-border/20">{{ item.rawText }}</pre>
        </div>
      </Panel>
    </div>
  </div>
</template>

<script setup>
import Button from 'primevue/button'
import Tag from 'primevue/tag'
import Panel from 'primevue/panel'

defineProps({
  item: {
    type: Object,
    required: true
  }
})

const emit = defineEmits(['close', 'view-in-graph'])

async function copyToClipboard(text) {
  try {
    await navigator.clipboard.writeText(text)
  } catch (err) {
    console.error('Failed to copy:', err)
  }
}
</script>
