<template>
  <div class="h-full flex flex-col rounded-xl border border-dark-border overflow-hidden bg-dark-panel/50">
    <!-- Header -->
    <div class="p-3 border-b border-dark-border flex items-center justify-between">
      <div>
        <h3 class="font-semibold font-mono">{{ item.key }}</h3>
        <Tag :value="item.type" severity="info" class="mt-1" />
      </div>
      <Button icon="pi pi-times" text rounded severity="secondary" @click="$emit('close')" />
    </div>

    <!-- Content -->
    <div class="flex-1 overflow-auto p-3">
      <!-- Location -->
      <div class="mb-4">
        <h4 class="text-sm font-medium text-gray-400 mb-1">Location</h4>
        <p class="text-sm font-mono break-all">{{ item.filePath }}</p>
        <p class="text-xs text-gray-500">Lines {{ item.lineStart }} - {{ item.lineEnd }}</p>
      </div>

      <!-- Quick Actions -->
      <div class="mb-4">
        <h4 class="text-sm font-medium text-gray-400 mb-2">Quick Actions</h4>
        <div class="flex flex-wrap gap-1">
          <Button label="Copy Key" size="small" severity="secondary" text @click="copyToClipboard(item.key)" />
          <Button label="Copy Path" size="small" severity="secondary" text @click="copyToClipboard(item.filePath)" />
          <Button label="View Graph" size="small" severity="secondary" text @click="$emit('view-in-graph', item)" />
        </div>
      </div>

      <!-- References (collapsible, collapsed by default) -->
      <div v-if="item.references && item.references.length > 0" class="mb-4">
        <button type="button" class="w-full flex items-center justify-between gap-2 text-left mb-1"
          @click="showRefs = !showRefs">
          <h4 class="text-sm font-medium text-gray-400">
            References ({{ item.references.length }})
          </h4>
          <i :class="showRefs ? 'pi pi-chevron-down' : 'pi pi-chevron-right'" class="text-gray-500 text-xs" />
        </button>
        <div v-show="showRefs" class="max-h-32 overflow-auto">
          <div v-for="(ref, idx) in item.references" :key="idx"
            class="text-xs p-2 mb-1 rounded bg-dark-border/30 font-mono flex items-start justify-between gap-2">
            <div class="min-w-0 flex-1">
              <span class="text-blue-400">{{ ref.targetKey }}</span>
              <span class="text-gray-500"> ({{ ref.targetType }})</span>
              <span class="text-gray-600 block">{{ ref.context }} @ {{ ref.sourceFile }}:{{ ref.sourceLine }}</span>
            </div>
            <Button icon="pi pi-copy" size="small" severity="secondary" text title="Copy key"
              @click.stop="copyToClipboard(ref.targetKey)" />
          </div>
        </div>
      </div>

      <!-- Raw Text (collapsible, collapsed by default) -->
      <div>
        <button type="button" class="w-full flex items-center justify-between gap-2 text-left mb-1"
          @click="showRawText = !showRawText">
          <h4 class="text-sm font-medium text-gray-400">Raw Text</h4>
          <div class="flex items-center gap-1">
            <Button icon="pi pi-copy" size="small" severity="secondary" text title="Copy Raw Text"
              @click.stop="copyToClipboard(item.rawText)" />
            <i :class="showRawText ? 'pi pi-chevron-down' : 'pi pi-chevron-right'" class="text-gray-500 text-xs" />
          </div>
        </button>
        <pre v-show="showRawText"
          class="text-xs font-mono bg-dark-border/20 p-2 rounded overflow-auto max-h-96 whitespace-pre-wrap">{{ item.rawText }}</pre>
      </div>
    </div>
  </div>
</template>

<script>
import Button from 'primevue/button'
import Tag from 'primevue/tag'

export default {
  name: 'ItemDetailPanel',
  components: { Button, Tag },
  props: {
    item: {
      type: Object,
      required: true
    }
  },
  emits: ['close', 'view-in-graph'],
  data() {
    return {
      showRefs: false,
      showRawText: false
    }
  },
  methods: {
    async copyToClipboard(text) {
      try {
        await navigator.clipboard.writeText(text)
      } catch (error) {
        console.error('Failed to copy:', error)
      }
    }
  }
}
</script>
