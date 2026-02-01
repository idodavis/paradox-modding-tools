<template>
  <div class="p-4">
    <!-- Input Panel -->
    <Card class="w-full max-w-full mb-4">
      <template #content>
        <h2 class="text-lg font-semibold mb-4">Object Inventory</h2>

        <!-- File Selector -->
        <FileSelector v-model="files" class="mb-4" />

        <!-- Type Selector -->
        <TypeSelector v-model="selectedTypes" :game="game" class="mb-4" />

        <!-- Action Buttons -->
        <div class="flex flex-wrap gap-2">
          <Button v-if="!loading" @click="extractInventory" :disabled="files.length === 0 || selectedTypes.length === 0"
            label="Extract Inventory" />
          <Button v-if="loading" @click="cancelExtraction" label="Cancel Extraction" severity="danger" />
          <Button @click="showExportImport = true" :disabled="loading" label="Export / Import" severity="secondary" />
          <Button @click="clearAll" :disabled="loading" label="Clear All" severity="danger" text />
        </div>
      </template>
    </Card>

    <!-- Results Area: table + item detail drawer -->
    <div v-if="Object.keys(inventory).length > 0" class="flex min-w-0 flex-col">
      <ResultsTable :inventory="inventory" :loading="loading" :filter-state="filterState" @select="onSelectItem" />
      <Drawer v-model:visible="drawerVisible" position="right" class="w-full! md:w-100! lg:w-150!"
        :header="selectedItem ? selectedItem.key : ''">
        <ItemDetails v-if="selectedItem" :item="selectedItem" @view-in-graph="viewItemInGraph" />
      </Drawer>
    </div>

    <!-- Empty State -->
    <div v-else class="flex-1 flex items-center justify-center">
      <div class="text-center text-(--p-surface-400)">
        <p class="text-lg mb-2">No inventory loaded</p>
        <p class="text-sm">Select files/folders and object types, then click "Extract Inventory"</p>
      </div>
    </div>

    <!-- Graph: fullscreen Drawer (only when open; item detail opens as Dialog on node click) -->
    <Drawer v-if="showGraph" :visible="true" position="full" header="Reference Graph"
      @update:visible="(v) => { if (!v) closeGraph() }">
      <div class="h-full min-h-0 flex flex-col overflow-hidden">
        <ReferenceGraph v-if="graphFocusItem" :inventory="inventory" :focus-item="graphFocusItem"
          @open-item="openItemFromGraph" />
      </div>
    </Drawer>

    <!-- Item detail Dialog on top of graph when a node is clicked (z-index above fullscreen Drawer) -->
    <Dialog v-if="graphDetailItem" :visible="true" modal :header="graphDetailItem?.key ?? 'Item details'" class="z-1300"
      :closable="true" @update:visible="(v) => { if (!v) graphDetailItem = null }">
      <ItemDetails v-if="graphDetailItem" :item="graphDetailItem" @close="graphDetailItem = null"
        @view-in-graph="(it) => { graphDetailItem = null; graphFocusItem = it }" />
    </Dialog>

    <!-- Export/Import Dialog: uses current filtered state from filterState (no internal filtering). -->
    <ExportImportDialog v-if="showExportImport" :inventory="inventory" :filtered-inventory="filteredInventoryForExport"
      @close="showExportImport = false" @import="onImport" />
  </div>
</template>

<script setup>
import { ref, reactive, computed, watch, onMounted } from 'vue'
import { GetSupportedGames, ExtractInventory, CancelExtraction } from '../../bindings/paradox-modding-tool/inventoryservice.js'
import { CollectFilesFromPaths } from '../../bindings/paradox-modding-tool/fileservice.js'
import { applyInventoryFilter, countInventoryItems, countReferences } from '../utils/inventory.js'
import FileSelector from '../components/FileSelector.vue'
import TypeSelector from '../components/inventory/TypeSelector.vue'
import ResultsTable from '../components/inventory/ResultsTable.vue'
import ItemDetails from '../components/inventory/ItemDetails.vue'
import ReferenceGraph from '../components/inventory/ReferenceGraph.vue'
import ExportImportDialog from '../components/inventory/ExportImportDialog.vue'

const game = ref('ck3')
const games = ref([])
const files = ref([])
const selectedTypes = ref([])
const inventory = ref({})
const loading = ref(false)
/** Filter state for table and export (single source of truth). Match modes use PrimeVue FilterMatchMode values. */
const filterState = reactive({
  keyText: '',
  keyMatchMode: 'contains',
  typeNames: [],
  refsValue: null,
  refsMatchMode: 'gte'
})
/** Precomputed filtered map for export when "Export filtered only" is checked. */
const filteredInventoryForExport = computed(() => applyInventoryFilter(inventory.value, filterState).map)
const selectedItem = ref(null)
const drawerVisible = ref(false)
const showGraph = ref(false)
const graphFocusItem = ref(null)
const graphDetailItem = ref(null)
const showExportImport = ref(false)

watch(selectedItem, (item) => {
  drawerVisible.value = !!item
})
watch(drawerVisible, (visible) => {
  if (!visible) selectedItem.value = null
})

onMounted(async () => {
  try {
    console.log('InventoryTool: Loading supported games...')
    games.value = await GetSupportedGames()
    console.log('InventoryTool: Loaded games:', games.value)
    if (games.value.length > 0 && !games.value.includes(game.value)) {
      game.value = games.value[0]
    }
  } catch (err) {
    console.error('Failed to load supported games:', err)
  }
})

function onGameChange() {
  console.log('Game changed to:', game.value)
  selectedTypes.value = []
  inventory.value = {}
  selectedItem.value = null
}

async function extractInventory() {
  console.log('extractInventory called', { files: files.value, types: selectedTypes.value })
  if (files.value.length === 0 || selectedTypes.value.length === 0) {
    console.log('Early return: no files or types selected')
    return
  }

  loading.value = true
  inventory.value = {}
  selectedItem.value = null

  try {
    console.log('Collecting files from paths...')
    const fileMap = await CollectFilesFromPaths(files.value)
    const collectedFiles = Object.values(fileMap || {})
    console.log('Collected', collectedFiles.length, 'files')

    if (collectedFiles.length === 0) {
      alert('No .txt files found in the selected paths')
      loading.value = false
      return
    }

    console.log('Extracting types:', selectedTypes.value, 'from', collectedFiles.length, 'files')
    const result = await ExtractInventory(game.value, collectedFiles, selectedTypes.value)
    inventory.value = result.inventory || {}

    filterState.typeNames = Object.keys(inventory.value)
    const totalItems = countInventoryItems(inventory.value)
    const totalRefs = countReferences(inventory.value)
    console.log('Extraction complete. Types:', filterTypes.value, 'Total items:', totalItems, 'Total refs:', totalRefs)
  } catch (err) {
    const msg = String(err?.message ?? err)
    if (msg.toLowerCase().includes('cancelled')) {
      console.log('Extraction cancelled by user')
      if (Object.keys(inventory.value).length > 0) {
        filterState.typeNames = Object.keys(inventory.value)
      }
    } else {
      console.error('Extraction error:', err)
      alert('Error extracting inventory: ' + msg)
    }
  } finally {
    loading.value = false
  }
}

function viewItemInGraph(item) {
  closeDrawer()
  graphFocusItem.value = item
  graphDetailItem.value = null
  showGraph.value = true
}

function onSelectItem(item) {
  selectedItem.value = item
  drawerVisible.value = !!item
}

function closeDrawer() {
  drawerVisible.value = false
  selectedItem.value = null
}

function cancelExtraction() {
  CancelExtraction()
}

function closeGraph() {
  showGraph.value = false
  graphFocusItem.value = null
  graphDetailItem.value = null
}

function openItemFromGraph(item) {
  graphDetailItem.value = item
}

function onImport(importedData) {
  const normalized = {}
  for (const [type, result] of Object.entries(importedData)) {
    const items = (result.items || []).map((item) => ({
      key: item.key,
      type: item.type ?? type,
      filePath: item.filePath ?? '',
      lineStart: item.lineStart,
      lineEnd: item.lineEnd,
      rawText: item.rawText ?? '',
      references: Array.isArray(item.references) ? item.references : [],
    }))
    normalized[type] = { type, totalCount: items.length, items }
  }
  if (Object.keys(inventory.value).length === 0) {
    inventory.value = { ...normalized }
  } else {
    for (const [type, result] of Object.entries(normalized)) {
      if (inventory.value[type]) {
        const existingKeys = new Set(inventory.value[type].items.map((i) => i.key))
        for (const item of result.items) {
          if (!existingKeys.has(item.key)) {
            inventory.value[type].items.push(item)
          }
        }
        inventory.value[type].totalCount = inventory.value[type].items.length
      } else {
        inventory.value[type] = result
      }
    }
  }
  filterState.typeNames = [...new Set([...filterState.typeNames, ...Object.keys(normalized)])]
  showExportImport.value = false
}

function clearAll() {
  inventory.value = {}
  selectedItem.value = null
  filterState.keyText = ''
  filterState.keyMatchMode = 'contains'
  filterState.typeNames = []
  filterState.refsValue = null
  filterState.refsMatchMode = 'gte'
}
</script>
