<template>
  <Dialog :visible="true" modal header="Export / Import Inventory" class="w-150 max-w-[90vw]"
    @update:visible="emit('close')" :closable="true">
    <Tabs v-model:value="activeTab">
      <TabList>
        <Tab value="export">Export</Tab>
        <Tab value="import">Import</Tab>
      </TabList>

      <TabPanels>
        <!-- Export Panel -->
        <TabPanel value="export">
          <div class="p-4">
            <h4 class="font-medium mb-3">Export Options</h4>

            <!-- Scope Options -->
            <div class="mb-4">
              <div class="flex items-center gap-2 mb-2">
                <Checkbox v-model="exportOptions.filteredOnly" inputId="filteredOnly" binary />
                <label for="filteredOnly" class="text-sm">Export filtered items only</label>
              </div>
              <div class="flex items-center gap-2">
                <Checkbox v-model="exportOptions.includeRawText" inputId="rawText" binary />
                <label for="rawText" class="text-sm">Include Raw Text</label>
              </div>
            </div>

            <!-- Format -->
            <div class="mb-4">
              <label class="block text-sm font-medium mb-2">Format</label>
              <div class="flex gap-4">
                <div class="flex items-center gap-2">
                  <RadioButton v-model="exportOptions.format" inputId="json" value="json" />
                  <label for="json" class="text-sm">JSON</label>
                </div>
                <div class="flex items-center gap-2">
                  <RadioButton v-model="exportOptions.format" inputId="csv" value="csv" />
                  <label for="csv" class="text-sm">CSV (flat)</label>
                </div>
              </div>
            </div>

            <!-- Stats -->
            <div class="mb-4 p-3 bg-dark-border/20 rounded text-sm">
              <p v-if="totalItems === 0" class="text-gray-400">No inventory to export. Import a JSON file or run
                extraction first.</p>
              <template v-else>
                <p><strong>{{ totalItems }}</strong> items across <strong>{{ Object.keys(exportSource).length
                    }}</strong>
                  types</p>
                <p v-if="exportOptions.filteredOnly && totalItems !== allItems" class="text-yellow-400 text-xs mt-1">
                  ({{ allItems - totalItems }} items excluded by filter)
                </p>
                <p class="text-gray-400 text-xs mt-1">Estimated size: {{ estimatedSize }}</p>
              </template>
            </div>

            <Button label="Export" @click="doExport" class="w-full" :disabled="totalItems === 0" />
          </div>
        </TabPanel>

        <!-- Import Panel -->
        <TabPanel value="import">
          <div class="p-4">
            <h4 class="font-medium mb-3">Import JSON Inventory</h4>

            <div class="mb-4">
              <input type="file" ref="fileInputRef" accept=".json" @change="onFileSelect" class="hidden" />
              <Button label="Select JSON File" @click="fileInputRef?.click()" class="w-full mb-2" />
              <p v-if="importFile" class="text-sm text-gray-400">Selected: {{ importFile.name }}</p>
            </div>

            <!-- Preview -->
            <div v-if="importPreview" class="mb-4 p-3 bg-dark-border/20 rounded text-sm">
              <p class="font-medium mb-2">Preview:</p>
              <p v-for="(count, type) in importPreview" :key="type">
                {{ type }}: {{ count }} items
              </p>
            </div>

            <Button label="Import" @click="doImport" :disabled="!importData" class="w-full" />
          </div>
        </TabPanel>
      </TabPanels>
    </Tabs>
  </Dialog>
</template>

<script setup>
import { ref, reactive, computed } from 'vue'
import Dialog from 'primevue/dialog'
import Tabs from 'primevue/tabs'
import TabList from 'primevue/tablist'
import Tab from 'primevue/tab'
import TabPanels from 'primevue/tabpanels'
import TabPanel from 'primevue/tabpanel'
import Checkbox from 'primevue/checkbox'
import RadioButton from 'primevue/radiobutton'
import Button from 'primevue/button'
import { countInventoryItems } from '../../utils/inventory.js'
import { SaveFile } from '../../../bindings/paradox-modding-tool/fileservice.js'

const props = defineProps({
  /** Full inventory (used when "Export filtered only" is unchecked). */
  inventory: {
    type: Object,
    required: true
  },
  /** Current filtered inventory (map shape). Used when "Export filtered only" is checked. */
  filteredInventory: {
    type: Object,
    default: () => ({})
  }
})

const emit = defineEmits(['close', 'import'])

const fileInputRef = ref(null)

const activeTab = ref(Object.keys(props.inventory).length === 0 ? 'import' : 'export')
const exportOptions = reactive({
  filteredOnly: false,
  includeRawText: true,
  format: 'json'
})
const importFile = ref(null)
const importData = ref(null)
const importPreview = ref(null)

/** Source for export: full inventory or current filtered inventory (no internal filtering). */
const exportSource = computed(() =>
  exportOptions.filteredOnly ? props.filteredInventory : props.inventory
)

const totalItems = computed(() => countInventoryItems(exportSource.value))
const allItems = computed(() => countInventoryItems(props.inventory))

const estimatedSize = computed(() => {
  let size = 0
  for (const result of Object.values(exportSource.value)) {
    for (const item of result.items) {
      size += item.key.length + item.filePath.length + 100
      if (exportOptions.includeRawText && item.rawText) {
        size += item.rawText.length
      }
    }
  }
  if (size < 1024) return `${size} B`
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`
  return `${(size / 1024 / 1024).toFixed(1)} MB`
})

function doExport() {
  const exportData = {}
  for (const [type, result] of Object.entries(exportSource.value)) {
    exportData[type] = {
      type: result.type,
      totalCount: result.items.length,
      items: result.items.map((item) => {
        const exported = {
          key: item.key,
          type: item.type,
          filePath: item.filePath,
          lineStart: item.lineStart,
          lineEnd: item.lineEnd
        }
        if (exportOptions.includeRawText) {
          exported.rawText = item.rawText
        }
        if (item.references) {
          exported.references = item.references
        }
        return exported
      })
    }
  }

  let content, filename, fileType
  if (exportOptions.format === 'json') {
    content = JSON.stringify(exportData, null, 2)
    filename = 'inventory.json'
    fileType = 'json'
  } else {
    const rows = [['key', 'type', 'filePath', 'lineStart', 'lineEnd']]
    for (const result of Object.values(exportData)) {
      for (const item of result.items) {
        rows.push([item.key, item.type, item.filePath, item.lineStart, item.lineEnd])
      }
    }
    content = rows.map((r) => r.map((c) => `"${String(c).replace(/"/g, '""')}"`).join(',')).join('\n')
    filename = 'inventory.csv'
    fileType = 'csv'
  }

  SaveFile(filename, fileType, content)
  emit('close')
}

function onFileSelect(event) {
  const file = event.target.files[0]
  if (!file) return

  importFile.value = file
  const reader = new FileReader()
  reader.onload = (e) => {
    try {
      importData.value = JSON.parse(e.target.result)
      importPreview.value = {}
      for (const [type, result] of Object.entries(importData.value)) {
        importPreview.value[type] = result.items?.length || 0
      }
    } catch (err) {
      alert('Invalid JSON file: ' + err.message)
      importData.value = null
      importPreview.value = null
    }
  }
  reader.readAsText(file)
}

function doImport() {
  if (importData.value) {
    emit('import', importData.value)
  }
}
</script>
