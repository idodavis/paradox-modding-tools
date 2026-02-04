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
      <ResultsTable :value="pageItemsWithIds" :total-records="totalRecords" :lazy="true"
        :first="(currentPage - 1) * pageSize" :rows="pageSize" filterDisplay="row" paginator
        :rowsPerPageOptions="[10, 25, 50, 100]" :loading="loading || pageLoading" selectionMode="single"
        v-model:selection="selectedRow" dataKey="uniqueId" :sortField="sortField" :sortOrder="sortOrder"
        :current-page="currentPage" :page-size="pageSize" :available-type-names="availableTypeNames"
        :filter-state="filterState" @select="onSelectItem" @filter-change="onFilterChange" @page="onPage" @sort="onSort"
        stripedRows />
      <Drawer v-model:visible="drawerVisible" position="right" class="w-full! md:w-100! lg:w-150!"
        :header="selectedItem ? selectedItem.key : ''">
        <ItemDetails v-if="selectedItem" :game="game" :item="selectedItem" />
      </Drawer>
    </div>

    <div v-else class="flex-1 flex items-center justify-center">
      <div class="text-center text-(--p-surface-400)">
        <p class="text-lg mb-2">No inventory loaded</p>
        <p class="text-sm">Select files/folders and object types, then click "Extract Inventory"</p>
      </div>
    </div>

    <ExportImportDialog v-if="showExportImport" :extract-result="extractResult" :filter-state="filterState"
      :has-extraction="hasExtraction" @close="showExportImport = false" @import="onImport" />
  </div>
</template>

<script setup>
import { ref, reactive, computed, watch } from 'vue'
import { ExtractInventory, CancelExtraction, GetFilteredSortedPage } from '../../bindings/paradox-modding-tool/inventoryservice.js'
import FileSelector from '../components/FileSelector.vue'
import TypeSelector from '../components/inventory/TypeSelector.vue'
import ResultsTable from '../components/inventory/ResultsTable.vue'
import ItemDetails from '../components/inventory/ItemDetails.vue'
import ExportImportDialog from '../components/inventory/ExportImportDialog.vue'
import { useAppSettings } from '../composables/useAppSettings'

const { game } = useAppSettings()
const files = ref([])
const selectedTypes = ref([])
const hasExtraction = ref(false)
const loading = ref(false)
const extractResult = ref(null)
const extractionErrors = ref([])
const sortField = ref('key')
const sortOrder = ref(1)
const currentPage = ref(1)
const pageSize = ref(25)
const availableTypeNames = ref([])
const filterState = reactive({
  keyText: '',
  keyMatchMode: 'contains',
  typeNames: [],
  refsValue: null,
  refsMatchMode: 'gte'
})
const selectedItem = ref(null)
const selectedRow = ref(null)
const drawerVisible = ref(false)
const showExportImport = ref(false)

const pageData = ref({ items: [], totalRecords: 0 })
const pageLoading = ref(false)

const totalRecords = computed(() => pageData.value.totalRecords)
const pageItemsWithIds = computed(() =>
  (pageData.value.items || []).map((item, i) => ({ ...item, uniqueId: `${item.type}-${item.key}-${i}` }))
)

async function loadPage() {
  if (!extractResult.value?.items) {
    pageData.value = { items: [], totalRecords: 0 }
    return
  }
  pageLoading.value = true
  try {
    const first = (currentPage.value - 1) * pageSize.value
    const result = await GetFilteredSortedPage(
      extractResult.value,
      {
        keyText: filterState.keyText,
        keyMatchMode: filterState.keyMatchMode || 'contains',
        typeNames: filterState.typeNames || [],
        refsValue: filterState.refsValue ?? null,
        refsMatchMode: filterState.refsMatchMode || 'gte'
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

watch(selectedItem, (item) => {
  drawerVisible.value = !!item
})
watch(drawerVisible, (visible) => {
  if (!visible) selectedItem.value = null
})
watch(
  [
    () => hasExtraction.value,
    () => extractResult.value,
    () => ({ ...filterState }),
    sortField,
    sortOrder,
    currentPage,
    pageSize
  ],
  () => {
    if (hasExtraction.value) loadPage()
  },
  { immediate: true, deep: true }
)

watch([game], () => {
  clearAll()
}, { immediate: true })

function onFilterChange() {
  currentPage.value = 1
}

function onSort(event) {
  sortField.value = event.sortField || 'key'
  sortOrder.value = event.sortOrder ?? 1
  currentPage.value = 1
}

function onPage(event) {
  const first = event.first ?? 0
  const rows = event.rows ?? pageSize.value
  pageSize.value = rows
  currentPage.value = Math.floor(first / rows) + 1
}

function onSelectItem(item) {
  selectedItem.value = item || null
  selectedRow.value = item || null
  drawerVisible.value = !!item
}

function onFiltersUpdate() {
  currentPage.value = 1
}

async function extractInventory() {
  if (files.value.length === 0 || selectedTypes.value.length === 0) return

  loading.value = true
  hasExtraction.value = false
  extractResult.value = null
  extractionErrors.value = []
  selectedItem.value = null
  selectedRow.value = null

  try {
    const result = await ExtractInventory(game.value, files.value, selectedTypes.value)
    if (result) {
      extractResult.value = result
      extractionErrors.value = result.errors || []
      const typeNames = result.items ? Object.keys(result.items) : []
      availableTypeNames.value = typeNames.sort()
      filterState.typeNames = typeNames
      hasExtraction.value = typeNames.length > 0
      currentPage.value = 1
    }
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

function onImport(importedData) {
  const items = {}
  for (const [type, result] of Object.entries(importedData)) {
    const list = (result?.items || []).map((item) => ({
      key: item.key,
      type: item.type ?? type,
      filePath: item.filePath ?? '',
      lineStart: item.lineStart,
      lineEnd: item.lineEnd,
      rawText: item.rawText ?? '',
      references: Array.isArray(item.references) ? item.references : []
    }))
    if (list.length) items[type] = list
  }
  if (Object.keys(items).length === 0) {
    showExportImport.value = false
    return
  }
  extractResult.value = { items, errors: [] }
  extractionErrors.value = []
  const newTypes = Object.keys(items)
  availableTypeNames.value = [...new Set([...availableTypeNames.value, ...newTypes])]
  filterState.typeNames = [...new Set([...filterState.typeNames, ...newTypes])]
  hasExtraction.value = true
  currentPage.value = 1
  showExportImport.value = false
}

function clearAll() {
  hasExtraction.value = false
  extractResult.value = null
  extractionErrors.value = []
  availableTypeNames.value = []
  pageData.value = { items: [], totalRecords: 0 }
  selectedItem.value = null
  selectedRow.value = null
  filterState.keyText = ''
  filterState.keyMatchMode = 'contains'
  filterState.typeNames = []
  filterState.refsValue = null
  filterState.refsMatchMode = 'gte'
  currentPage.value = 1
}
</script>
