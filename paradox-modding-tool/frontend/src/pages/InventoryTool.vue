<template>
  <div class="p-4">
    <!-- Input Panel -->
    <Card class="w-full max-w-full mb-4">
      <template #content>
        <h2 class="text-lg font-semibold mb-4">Object Inventory</h2>

        <FileSelector v-model="files" class="mb-4" />

        <TypeSelector v-model="selectedTypes" :game="game" class="mb-4" />

        <div class="flex flex-wrap gap-2">
          <Button v-if="!loading" @click="extractInventory" :disabled="files.length === 0 || selectedTypes.length === 0"
            label="Extract Inventory" />
          <Button v-if="loading" @click="cancelExtraction" label="Cancel Extraction" severity="danger" />
          <Button @click="showExportImport = true" :disabled="loading" label="Export / Import" severity="secondary" />
          <Button @click="clearAll" :disabled="loading" label="Clear All" severity="danger" text />
        </div>
      </template>
    </Card>

    <!-- Extraction errors (if any) -->
    <Message v-if="extractionErrors.length > 0" severity="warn" :closable="false" class="mb-4">
      <p class="font-medium mb-1">Parse errors during extraction ({{ extractionErrors.length }}):</p>
      <ul class="list-disc pl-5 text-sm max-h-32 overflow-auto">
        <li v-for="(err, i) in extractionErrors" :key="i">{{ err }}</li>
      </ul>
    </Message>

    <!-- Results Area -->
    <div v-if="hasExtraction" class="flex min-w-0 flex-col">
      <ResultsTable :value="pageData.items" :total-records="pageData.totalRecords" :lazy="true"
        :first="(currentPage - 1) * pageSize" :rows="pageSize" filterDisplay="row" paginator
        :rowsPerPageOptions="[10, 25, 50, 100]" :loading="loading || pageLoading" selectionMode="single"
        dataKey="uniqueId" :sortField="sortField" :sortOrder="sortOrder"
        :current-page="currentPage" :page-size="pageSize" :available-type-names="availableTypeNames"
        :filter-state="filterState" :game="game" @page="onPage" @sort="onSort" stripedRows />
    </div>

    <div v-else class="flex-1 flex items-center justify-center">
      <div class="text-center text-(--p-surface-400)">
        <p class="text-lg mb-2">No inventory loaded</p>
        <p class="text-sm">Select files/folders and object types, then click "Extract Inventory"</p>
      </div>
    </div>

    <ExportImportDialog v-if="showExportImport" :filter-state="filterState" :has-extraction="hasExtraction"
      @close="showExportImport = false" @import="onImport" />
  </div>
</template>

<script setup>
import { ref, reactive, watch } from 'vue'
import { ExtractInventory, CancelExtraction, GetFilteredSortedPage } from '../../bindings/paradox-modding-tool/inventoryservice.js'
import FileSelector from '../components/FileSelector.vue'
import TypeSelector from '../components/inventory/TypeSelector.vue'
import ResultsTable from '../components/inventory/ResultsTable.vue'
import ExportImportDialog from '../components/inventory/ExportImportDialog.vue'
import { useAppSettings } from '../composables/useAppSettings'

const { game } = useAppSettings()
const files = ref([])
const selectedTypes = ref([])
const hasExtraction = ref(false)
const loading = ref(false)
const extractionErrors = ref([])
const sortField = ref('key')
const sortOrder = ref(-1)
const currentPage = ref(1)
const pageSize = ref(25)
const availableTypeNames = ref([])
const filterState = reactive({
  keyText: '',
  keyMatchMode: 'CONTAINS',
  typeNames: [],
  refsValue: null,
  refsMatchMode: 'GREATER_THAN_OR_EQUAL_TO'
})
const showExportImport = ref(false)

const pageData = ref({ items: [], totalRecords: 0 })
const pageLoading = ref(false)

async function loadPage() {
  if (!hasExtraction.value) {
    pageData.value = { items: [], totalRecords: 0 }
    return
  }
  pageLoading.value = true
  try {
    const first = (currentPage.value - 1) * pageSize.value
    const result = await GetFilteredSortedPage(
      {
        keyText: filterState.keyText,
        keyMatchMode: filterState.keyMatchMode || 'CONTAINS',
        typeNames: filterState.typeNames || [],
        refsValue: filterState.refsValue ?? null,
        refsMatchMode: filterState.refsMatchMode || 'GREATER_THAN_OR_EQUAL_TO'
      },
      sortField.value || 'key',
      sortOrder.value ?? 1,
      first,
      pageSize.value
    )
    pageData.value = result
      ? { items: result.items || [], totalRecords: result.totalRecords ?? 0 }
      : { items: [], totalRecords: 0 }
  } catch (err) {
    console.error('Load page error:', err)
    pageData.value = { items: [], totalRecords: 0 }
  } finally {
    pageLoading.value = false
  }
}

watch([hasExtraction, () => filterState.keyText, () => filterState.keyMatchMode, () => filterState.typeNames, () => filterState.refsValue, () => filterState.refsMatchMode, sortField, sortOrder, currentPage, pageSize], () => {
  if (hasExtraction.value) loadPage()
}, { immediate: true })

watch([() => filterState.keyText, () => filterState.keyMatchMode, () => filterState.typeNames, () => filterState.refsValue, () => filterState.refsMatchMode, sortField, sortOrder], () => {
  currentPage.value = 1
})

watch(game, clearAll, { immediate: true })

function onSort(event) {
  sortField.value = event.sortField || 'key'
  sortOrder.value = event.sortOrder ?? 1
}

function onPage(event) {
  pageSize.value = event.rows ?? pageSize.value
  currentPage.value = Math.floor((event.first ?? 0) / pageSize.value) + 1
}

async function extractInventory() {
  if (files.value.length === 0 || selectedTypes.value.length === 0) return

  loading.value = true
  hasExtraction.value = false
  extractionErrors.value = []
  pageData.value = { items: [], totalRecords: 0 }

  try {
    await ExtractInventory(game.value, files.value, selectedTypes.value)
    // After extraction succeeds, load the first page
    hasExtraction.value = true
    currentPage.value = 1
    filterState.typeNames = []
    // Load the initial page
    await loadPage()
    // Extract type names from the first page items (temporary until ExtractInventory returns summary)
    const typeSet = new Set()
    pageData.value.items.forEach(item => typeSet.add(item.type))
    availableTypeNames.value = Array.from(typeSet).sort()
  } catch (err) {
    const msg = String(err?.message ?? err)
    if (msg.toLowerCase().includes('cancelled')) {
      clearAll()
    } else {
      console.error('Extraction error:', err)
      alert('Error extracting inventory: ' + msg)
    }
  } finally {
    loading.value = false
  }
}

function cancelExtraction() {
  CancelExtraction()
}

async function onImport(importedData) {
  // For now, this is a placeholder - import should call backend ImportInventory API
  console.warn('Import functionality needs to be updated to use backend ImportInventory API')
  showExportImport.value = false
}

function clearAll() {
  hasExtraction.value = false
  extractionErrors.value = []
  availableTypeNames.value = []
  pageData.value = { items: [], totalRecords: 0 }
  filterState.keyText = ''
  filterState.keyMatchMode = 'CONTAINS'
  filterState.typeNames = []
  filterState.refsValue = null
  filterState.refsMatchMode = 'GREATER_THAN_OR_EQUAL_TO'
  currentPage.value = 1
}
</script>
