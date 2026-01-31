<template>
  <Dialog :visible="true" modal header="Export / Import Inventory" :style="{ width: '37.5rem', maxWidth: '90vw' }"
    @update:visible="$emit('close')" :closable="true">
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
              <p v-if="totalItems === 0" class="text-gray-400">No inventory to export. Import a JSON file or run extraction first.</p>
              <template v-else>
                <p><strong>{{ totalItems }}</strong> items across <strong>{{ Object.keys(filteredInventory).length }}</strong>
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
              <input type="file" ref="fileInput" accept=".json" @change="onFileSelect" class="hidden" />
              <Button label="Select JSON File" @click="$refs.fileInput.click()" class="w-full mb-2" />
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

<script>
import Dialog from 'primevue/dialog'
import Tabs from 'primevue/tabs'
import TabList from 'primevue/tablist'
import Tab from 'primevue/tab'
import TabPanels from 'primevue/tabpanels'
import TabPanel from 'primevue/tabpanel'
import Checkbox from 'primevue/checkbox'
import RadioButton from 'primevue/radiobutton'
import Button from 'primevue/button'
import { filterInventory, countInventoryItems } from '../../utils/inventory.js'

export default {
  name: 'ExportImportDialog',
  components: { Dialog, Tabs, TabList, Tab, TabPanels, TabPanel, Checkbox, RadioButton, Button },
  props: {
    inventory: {
      type: Object,
      required: true
    },
    filterText: {
      type: String,
      default: ''
    },
    filterTypes: {
      type: Array,
      default: () => []
    }
  },
  emits: ['close', 'import'],
  data() {
    return {
      // Default to Import when no inventory so user can import before extraction
      activeTab: Object.keys(this.inventory).length === 0 ? 'import' : 'export',
      exportOptions: {
        filteredOnly: false,
        includeRawText: false,
        format: 'json'
      },
      importFile: null,
      importData: null,
      importPreview: null
    }
  },
  computed: {
    filteredInventory() {
      if (!this.exportOptions.filteredOnly) return this.inventory
      return filterInventory(this.inventory, {
        filterText: this.filterText,
        filterTypes: this.filterTypes,
      })
    },
    totalItems() {
      return countInventoryItems(this.filteredInventory)
    },
    allItems() {
      return countInventoryItems(this.inventory)
    },
    estimatedSize() {
      let size = 0
      for (const result of Object.values(this.filteredInventory)) {
        for (const item of result.items) {
          size += item.key.length + item.filePath.length + 100
          if (this.exportOptions.includeRawText && item.rawText) {
            size += item.rawText.length
          }
        }
      }
      if (size < 1024) return `${size} B`
      if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`
      return `${(size / 1024 / 1024).toFixed(1)} MB`
    }
  },
  methods: {
    doExport() {
      const exportData = {}

      for (const [type, result] of Object.entries(this.filteredInventory)) {
        exportData[type] = {
          type: result.type,
          totalCount: result.items.length,
          items: result.items.map(item => {
            const exported = {
              key: item.key,
              type: item.type,
              filePath: item.filePath,
              lineStart: item.lineStart,
              lineEnd: item.lineEnd
            }
            if (this.exportOptions.includeRawText) {
              exported.rawText = item.rawText
            }
            if (item.references) {
              exported.references = item.references
            }
            return exported
          })
        }
      }

      let content, filename, mimeType
      if (this.exportOptions.format === 'json') {
        content = JSON.stringify(exportData, null, 2)
        filename = 'inventory.json'
        mimeType = 'application/json'
      } else {
        // CSV export (flat)
        const rows = [['key', 'type', 'filePath', 'lineStart', 'lineEnd']]
        for (const result of Object.values(exportData)) {
          for (const item of result.items) {
            rows.push([item.key, item.type, item.filePath, item.lineStart, item.lineEnd])
          }
        }
        content = rows.map(r => r.map(c => `"${String(c).replace(/"/g, '""')}"`).join(',')).join('\n')
        filename = 'inventory.csv'
        mimeType = 'text/csv'
      }

      // Download
      const blob = new Blob([content], { type: mimeType })
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = filename
      a.click()
      URL.revokeObjectURL(url)

      this.$emit('close')
    },
    onFileSelect(event) {
      const file = event.target.files[0]
      if (!file) return

      this.importFile = file
      const reader = new FileReader()
      reader.onload = (e) => {
        try {
          this.importData = JSON.parse(e.target.result)
          // Build preview
          this.importPreview = {}
          for (const [type, result] of Object.entries(this.importData)) {
            this.importPreview[type] = result.items?.length || 0
          }
        } catch (error) {
          alert('Invalid JSON file: ' + error.message)
          this.importData = null
          this.importPreview = null
        }
      }
      reader.readAsText(file)
    },
    doImport() {
      if (this.importData) {
        this.$emit('import', this.importData)
      }
    }
  }
}
</script>
