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
              <p v-if="!hasExtraction" class="text-(--p-surface-400)">No inventory to export. Import a JSON file or run extraction first.</p>
              <p v-else class="text-(--p-surface-400)">
                Export from current extraction.
                <span v-if="exportOptions.filteredOnly">Only items matching the current table filter will be exported.</span>
              </p>
            </div>

            <Button label="Export" @click="doExport" class="w-full" :disabled="!hasExtraction || exporting"
              :loading="exporting" />
          </div>
        </TabPanel>

        <!-- Import Panel -->
        <TabPanel value="import">
          <div class="p-4">
            <h4 class="font-medium mb-3">Import JSON Inventory</h4>

            <div class="mb-4">
              <input type="file" ref="fileInputRef" accept=".json" @change="onFileSelect" class="hidden" />
              <Button label="Select JSON File" @click="fileInputRef?.click()" class="w-full mb-2" />
              <p v-if="importFile" class="text-sm text-(--p-surface-400)">Selected: {{ importFile.name }}</p>
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
import { ref, reactive } from 'vue'
import { ExportInventoryFromBackend, OpenFolder } from '../../../bindings/paradox-modding-tool/fileservice.js'

const props = defineProps({
  extractResult: {
    type: Object,
    default: null
  },
  filterState: {
    type: Object,
    required: true
  },
  hasExtraction: {
    type: Boolean,
    default: false
  }
})

const emit = defineEmits(['close', 'import'])

const fileInputRef = ref(null)

const activeTab = ref(props.hasExtraction ? 'export' : 'import')
const exportOptions = reactive({
  filteredOnly: false,
  includeRawText: true,
  format: 'json'
})
const importFile = ref(null)
const importData = ref(null)
const importPreview = ref(null)
const exporting = ref(false)

function dirFromPath(pathStr) {
  const sep = pathStr.includes('\\') ? '\\' : '/'
  const i = pathStr.lastIndexOf(sep)
  return i > 0 ? pathStr.slice(0, i) : pathStr
}

async function doExport() {
  if (!props.hasExtraction || !props.extractResult) return
  exporting.value = true
  try {
    const path = await ExportInventoryFromBackend(
      props.extractResult,
      props.filterState,
      exportOptions.format,
      exportOptions.includeRawText
    )
    if (path) {
      const msg = `Exported to:\n${path}`
      if (confirm(`${msg}\n\nOpen folder?`)) {
        await OpenFolder(dirFromPath(path))
      }
      emit('close')
    }
  } catch (err) {
    alert('Export failed: ' + (err?.message ?? err))
  } finally {
    exporting.value = false
  }
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
