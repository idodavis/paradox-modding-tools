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
        <span class="text-sm font-medium text-(--p-surface-400) block mb-1">Location</span>
        <p class="text-sm font-mono break-all">{{ item.filePath }}</p>
        <p class="text-xs text-(--p-surface-500)">Lines {{ item.lineStart }} - {{ item.lineEnd }}</p>
      </div>

      <!-- Quick Actions -->
      <div class="mb-4">
        <span class="text-sm font-medium text-(--p-surface-400) block mb-2">Quick Actions</span>
        <div class="flex flex-wrap gap-1">
          <Button label="Copy Key" size="small" severity="secondary" @click="copyToClipboard(item.key)" />
          <Button label="Copy Path" size="small" severity="secondary" @click="copyToClipboard(item.filePath)" />
        </div>
      </div>

      <!-- Fields / Sub-objects: field keys from type schema; ✓ = present in this object -->
      <Panel v-if="fieldsTable.length > 0" header="Fields / Sub-objects" toggleable class="mb-4">
        <p class="text-xs text-(--p-surface-500) mb-2">Expected fields for this type; ✓ = present in this object.</p>
        <DataTable :value="fieldsTable" size="small" class="text-sm" stripedRows>
          <Column field="key" header="Field" class="min-w-40">
            <template #body="{ data }">
              <span class="font-mono text-xs">{{ data.key }}</span>
            </template>
          </Column>
          <Column field="present" header="Present" class="w-20">
            <template #body="{ data }">
              <i v-if="data.present" class="pi pi-check text-(--p-green-400)" aria-hidden="true" />
              <span v-else class="text-(--p-surface-500)">—</span>
            </template>
          </Column>
        </DataTable>
      </Panel>

      <!-- References: PrimeVue Panel (toggleable); content constrained and scrollable -->
      <Panel v-if="item.references && item.references.length > 0" :header="`References (${item.references.length})`"
        toggleable class="mb-4 ">
        <div class="flex flex-col gap-2 min-h-0 overflow-auto max-h-56 ">
          <div v-for="(ref, idx) in item.references" :key="idx"
            class="flex items-start justify-between gap-2 p-2 rounded text-sm font-mono bg-dark-input">
            <div class="min-w-0 flex-1 wrap-break-word">
              <span class="text-(--p-primary-400)">{{ ref.targetKey }}</span>
              <span class="text-(--p-surface-500)"> ({{ ref.targetType }})</span>
              <span class="text-(--p-surface-600) block text-xs break-all">{{ ref.sourceFile }}:{{
                ref.sourceLine }}</span>
            </div>
            <Button icon="pi pi-copy" size="small" severity="secondary" outlined aria-label="Copy key"
              @click.stop="copyToClipboard(ref.sourceFile)" />
          </div>
        </div>
      </Panel>

      <!-- Raw Text: from item.rawText (set at extraction); unavailable for imported items that omit it -->
      <Panel header="Raw Text" toggleable class="mb-4 flex flex-col min-h-0">
        <div v-if="rawTextUnavailable" class="text-sm text-(--p-surface-500) p-2">
          Raw text is unavailable for this item (e.g. imported without raw text).
        </div>
        <CodeViewer v-else class="border border-(--p-surface-800) rounded-lg max-h-120" :content="displayRawText"
          :filename="fileNameFromPath(item.filePath)" />
      </Panel>
    </div>
  </div>
</template>

<script setup>
import { ref, watch, computed } from 'vue'
import { GetAttributes } from '../../../bindings/paradox-modding-tool/inventoryservice.js'
import { fileNameFromPath } from '../../utils/general.js'

const props = defineProps({
  game: {
    type: String,
    default: 'ck3'
  },
  item: {
    type: Object,
    required: true
  }
})

const emit = defineEmits(['close'])

const typeFields = ref([])
const rawTextUnavailable = computed(() => !props.item?.rawText || props.item.rawText.length === 0)
const displayRawText = computed(() => props.item?.rawText ?? '')

watch(
  () => [props.game, props.item?.type],
  async ([game, type]) => {
    if (!game || !type) {
      typeFields.value = []
      return
    }
    try {
      const fields = await GetAttributes(game, type)
      typeFields.value = fields || []
    } catch (err) {
      console.error('Failed to load type schema:', err)
      typeFields.value = []
    }
  },
  { immediate: true }
)

/** Present keys from item.attributes (set during extraction); fallback to empty for imported items. */
const presentAttributes = computed(() => {
  const attrs = props.item?.attributes
  if (!attrs || typeof attrs !== 'object') return new Set()
  return new Set(Object.keys(attrs).filter((k) => attrs[k]))
})

const fieldsTable = computed(() =>
  typeFields.value.map((key) => ({
    key,
    present: presentAttributes.value.has(key)
  }))
)

async function copyToClipboard(text) {
  try {
    await navigator.clipboard.writeText(text)
  } catch (err) {
    console.error('Failed to copy:', err)
  }
}
</script>
