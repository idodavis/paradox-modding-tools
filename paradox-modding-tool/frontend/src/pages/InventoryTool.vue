<template>
  <div class="flex-1 flex flex-col min-h-0 min-w-0 p-4 overflow-auto">
    <!-- Input Panel -->
    <div class="w-full max-w-full rounded-xl p-4 border border-dark-border mb-4">
      <h2 class="text-lg font-semibold mb-4">Object Inventory</h2>

      <!-- Game Selector -->
      <div class="mb-4">
        <label class="block mb-2 font-medium text-sm">Game:</label>
        <Select v-model="game" :options="games" placeholder="Select a game" class="w-full md:w-56"
          @change="onGameChange" />
      </div>

      <!-- File Selector -->
      <FileSelector v-model="files" class="mb-4" />

      <!-- Type Selector -->
      <TypeSelector v-model="selectedTypes" :game="game" class="mb-4" />

      <!-- Action Buttons -->
      <div class="flex flex-wrap gap-2">
        <Button @click="extractInventory" :disabled="loading || files.length === 0 || selectedTypes.length === 0"
          :label="loading ? 'Extracting...' : 'Extract Inventory'" />
        <Button @click="showGraph = true" :disabled="Object.keys(inventory).length === 0" label="View Graph"
          severity="secondary" />
        <Button @click="showExportImport = true" label="Export / Import" severity="secondary" />
        <Button @click="clearAll" label="Clear All" severity="danger" text />
      </div>
    </div>

    <!-- Results Area -->
    <div v-if="Object.keys(inventory).length > 0" class="flex flex-1 gap-4 min-h-150">
      <!-- Results Table -->
      <div class="flex-1 min-w-0 flex flex-col">
        <ResultsTable :inventory="inventory" :filter-text="filterText" :filter-types="filterTypes"
          @select="selectedItem = $event" @update:filter-text="filterText = $event"
          @update:filter-types="filterTypes = $event" class="flex-1" />
      </div>

      <!-- Detail Panel -->
      <div v-if="selectedItem" class="w-96">
        <ItemDetailPanel :item="selectedItem" @close="selectedItem = null" @view-in-graph="viewItemInGraph" />
      </div>
    </div>

    <!-- Empty State -->
    <div v-else class="flex-1 flex items-center justify-center">
      <div class="text-center text-gray-400">
        <p class="text-lg mb-2">No inventory loaded</p>
        <p class="text-sm">Select files/folders and object types, then click "Extract Inventory"</p>
      </div>
    </div>

    <!-- Reference Graph Modal -->
    <ReferenceGraph v-if="showGraph" :inventory="inventory" :graph="graph" :focus-item="graphFocusItem"
      @close="showGraph = false" />

    <!-- Export/Import Dialog: can open before extraction to import only -->
    <ExportImportDialog v-if="showExportImport" :inventory="inventory" :filter-text="filterText"
      :filter-types="filterTypes" @close="showExportImport = false" @import="onImport" />
  </div>
</template>

<script>
import { GetSupportedGames, GetSupportedTypes, ExtractInventory } from '../../bindings/paradox-modding-tool/inventoryservice.js'
import { CollectFilesFromPaths } from '../../bindings/paradox-modding-tool/fileservice.js'
import { countInventoryItems, countReferences } from '../utils/inventory.js'
import Button from 'primevue/button'
import Select from 'primevue/select'
import FileSelector from '../components/FileSelector.vue'
import TypeSelector from '../components/inventory/TypeSelector.vue'
import ResultsTable from '../components/inventory/ResultsTable.vue'
import ItemDetailPanel from '../components/inventory/ItemDetailPanel.vue'
import ReferenceGraph from '../components/inventory/ReferenceGraph.vue'
import ExportImportDialog from '../components/inventory/ExportImportDialog.vue'

export default {
  name: 'InventoryTool',
  components: {
    Button,
    Select,
    FileSelector,
    TypeSelector,
    ResultsTable,
    ItemDetailPanel,
    ReferenceGraph,
    ExportImportDialog,
  },
  data() {
    return {
      // Input state
      game: 'ck3',
      games: [],
      files: [],
      selectedTypes: [],

      // Results state
      inventory: {}, // Map<string, InventoryResult> - includes references on items
      graph: null,   // { nodes, links } from Go (precomputed reference graph)
      loading: false,

      // UI state
      filterText: '',
      filterTypes: [],
      selectedItem: null,
      showGraph: false,
      graphFocusItem: null,
      showExportImport: false,
    }
  },
  async mounted() {
    try {
      console.log('InventoryTool: Loading supported games...')
      this.games = await GetSupportedGames()
      console.log('InventoryTool: Loaded games:', this.games)
      if (this.games.length > 0 && !this.games.includes(this.game)) {
        this.game = this.games[0]
      }
    } catch (error) {
      console.error('Failed to load supported games:', error)
    }
  },
  methods: {
    onGameChange() {
      console.log('Game changed to:', this.game)
      this.selectedTypes = []
      this.inventory = {}
      this.graph = null
      this.selectedItem = null
    },
    async extractInventory() {
      console.log('extractInventory called', { files: this.files, types: this.selectedTypes })
      if (this.files.length === 0 || this.selectedTypes.length === 0) {
        console.log('Early return: no files or types selected')
        return
      }

      this.loading = true
      this.inventory = {}
      this.graph = null
      this.selectedItem = null

      try {
        // First, collect all .txt files from the selected paths (handles directories)
        console.log('Collecting files from paths...')
        const fileMap = await CollectFilesFromPaths(this.files)
        const collectedFiles = Object.values(fileMap || {})
        console.log('Collected', collectedFiles.length, 'files')

        if (collectedFiles.length === 0) {
          alert('No .txt files found in the selected paths')
          this.loading = false
          return
        }

        // Extract all types with references and precomputed graph in one call
        console.log('Extracting types:', this.selectedTypes, 'from', collectedFiles.length, 'files')
        const result = await ExtractInventory(this.game, collectedFiles, this.selectedTypes)
        this.inventory = result.inventory || {}
        this.graph = result.graph || null

        // Set filter types to all extracted types
        this.filterTypes = Object.keys(this.inventory)
        const totalItems = countInventoryItems(this.inventory)
        const totalRefs = countReferences(this.inventory)
        console.log('Extraction complete. Types:', this.filterTypes, 'Total items:', totalItems, 'Total refs:', totalRefs)
      } catch (error) {
        console.error('Extraction error:', error)
        alert('Error extracting inventory: ' + error)
      } finally {
        this.loading = false
      }
    },
    viewItemInGraph(item) {
      this.graphFocusItem = item
      this.showGraph = true
    },
    onImport(importedData) {
      // Normalize and load: tolerate missing rawText / references, then set or merge into inventory
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
      // Replace or merge: if we had no inventory, replace; else merge by key
      if (Object.keys(this.inventory).length === 0) {
        this.inventory = { ...normalized }
        this.graph = null
      } else {
        for (const [type, result] of Object.entries(normalized)) {
          if (this.inventory[type]) {
            const existingKeys = new Set(this.inventory[type].items.map((i) => i.key))
            for (const item of result.items) {
              if (!existingKeys.has(item.key)) {
                this.inventory[type].items.push(item)
              }
            }
            this.inventory[type].totalCount = this.inventory[type].items.length
          } else {
            this.inventory[type] = result
          }
        }
      }
      this.filterTypes = [...new Set([...this.filterTypes, ...Object.keys(normalized)])]
      this.showExportImport = false
    },
    clearAll() {
      this.inventory = {}
      this.graph = null
      this.selectedItem = null
      this.filterText = ''
      this.filterTypes = []
    }
  }
}
</script>
