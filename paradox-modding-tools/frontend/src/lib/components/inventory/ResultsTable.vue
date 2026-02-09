<template>
  <div class="flex flex-col rounded-xl border border-dark-border overflow-hidden overscroll-none">
    <DataTable :value="value" :totalRecords="totalRecords" :lazy="true" :first="(currentPage - 1) * pageSize"
      :rows="pageSize" :filters="filtersForDataTable" filterDisplay="row" paginator
      :rowsPerPageOptions="[10, 25, 50, 100]" :loading="loading" selectionMode="single" v-model:selection="selectedRow"
      dataKey="uniqueId" :sortField="sortField" :sortOrder="sortOrder" @update:filters="onFiltersUpdate" @page="onPage"
      @sort="onSort" stripedRows>
      <template #empty> No objects found. </template>
      <template #loading>
        <div class="flex items-center gap-3 p-4">
          <ProgressSpinner style="width: 28px; height: 28px" strokeWidth="4" />
          <span>Loading inventory data. Please wait.</span>
        </div>
      </template>
      <Column field="key" header="Key" sortable dataType="text" showFilterMenu showFilterMatchModes
        :filterMatchModeOptions="keyMatchModeOptions" class="min-w-50">
        <template #body="{ data }">
          <span class="font-mono text-sm">{{ data.key }}</span>
        </template>
        <template #filter>
          <div class="flex flex-col gap-1">
            <InputText :modelValue="keyTextLocal" @update:modelValue="onKeyFilterInput($event)" type="text" zzz
              placeholder="Search by key" class="w-full min-w-0" />
          </div>
        </template>
      </Column>
      <Column field="type" header="Type" sortable showFilterMenu class="w-37 min-w-0">
        <template #body="{ data }">
          <Tag :value="data.type" severity="success" />
        </template>
        <template #filter>
          <MultiSelect :modelValue="filterState.typeNames" @update:modelValue="filterState.typeNames = $event"
            :options="filterTypeOptions" optionLabel="type" optionValue="type" placeholder="All types"
            :maxSelectedLabels="3" filter filterPlaceholder="Type to filter..."
            class="w-full min-w-0 [&_.p-multiselect-label]:min-h-0 [&_.p-multiselect-trigger]:overflow-hidden [&_.p-multiselect-trigger]:max-h-10"
            panelClass="max-h-56 overflow-auto">
            <template #option="slotProps">
              <div class="flex items-center gap-2">
                <Tag :value="slotProps.option.type" severity="success" />
              </div>
            </template>
          </MultiSelect>
        </template>
      </Column>
      <Column field="filePath" header="File" class="min-w-62">
        <template #body="{ data }">
          <span class="text-sm text-(--p-surface-300) truncate" :title="data.filePath">{{ shortenPath(data.filePath)
            }}</span>
        </template>
      </Column>
      <Column header="Lines" class="w-25">
        <template #body="{ data }">
          <span class="text-sm text-(--p-surface-400)">{{ data.lineStart }}-{{ data.lineEnd }}</span>
        </template>
      </Column>
      <Column field="references" header="Refs" sortable dataType="numeric" showFilterMenu showFilterMatchModes
        :filterMatchModeOptions="refsMatchModeOptions" class="w-20">
        <template #body="{ data }">
          <span class="text-sm text-(--p-primary-400)">
            {{ (data.references && data.references.length) || 0 }}
          </span>
        </template>
        <template #filter>
          <div class="flex flex-col gap-1">
            <InputNumber :modelValue="filterState.refsValue" @update:modelValue="filterState.refsValue = $event"
              placeholder="Refs" class="w-full min-w-0" :min="0" />
          </div>
        </template>
      </Column>
    </DataTable>
    <Drawer v-model:visible="drawerVisible" position="right" class="w-full! md:w-100! lg:w-150!"
      :header="selectedItem ? selectedItem.key : ''">
      <ItemDetails v-if="selectedItem" :game="game" :item="selectedItem" />
    </Drawer>
  </div>
</template>

<script setup>
import { ref, computed, watch } from 'vue'
import { FilterMatchMode } from '@primevue/core/api'
import ProgressSpinner from 'primevue/progressspinner'
import Drawer from 'primevue/drawer'
import ItemDetails from './ItemDetails.vue'
import { shortenPath } from '../../utils/general.js'

const DEBOUNCE_KEY_MS = 200

function useDebounceFn(fn, ms) {
  let timeout = null
  return (...args) => {
    if (timeout) clearTimeout(timeout)
    timeout = setTimeout(() => { fn(...args); timeout = null }, ms)
  }
}

const keyMatchModeOptions = [
  { label: 'Contains', value: FilterMatchMode.CONTAINS },
  { label: 'Equals', value: FilterMatchMode.EQUALS },
  { label: 'Not contains', value: FilterMatchMode.NOT_CONTAINS },
  { label: 'Not equals', value: FilterMatchMode.NOT_EQUALS }
]

const refsMatchModeOptions = [
  { label: '≥', value: FilterMatchMode.GREATER_THAN_OR_EQUAL_TO },
  { label: '=', value: FilterMatchMode.EQUALS },
  { label: '≤', value: FilterMatchMode.LESS_THAN_OR_EQUAL_TO },
]


const props = defineProps({
  /** Current page items (with uniqueId for dataKey). */
  value: {
    type: Array,
    default: () => []
  },
  totalRecords: {
    type: Number,
    default: 0
  },
  loading: {
    type: Boolean,
    default: false
  },
  filterState: {
    type: Object,
    required: true
  },
  /** All type names from extraction (for type filter dropdown). */
  availableTypeNames: {
    type: Array,
    default: () => []
  },
  sortField: {
    type: String,
    default: null
  },
  sortOrder: {
    type: Number,
    default: null
  },
  currentPage: {
    type: Number,
    default: 1
  },
  pageSize: {
    type: Number,
    default: 25
  },
  game: {
    type: String,
    required: true
  }
})

const emit = defineEmits(['sort', 'page'])

const selectedRow = ref(null)
const selectedItem = ref(null)
const drawerVisible = ref(false)
const keyTextLocal = ref(props.filterState.keyText || '')

const filterTypeOptions = computed(() =>
  props.availableTypeNames.slice().sort().map((type) => ({ type }))
)

const filtersForDataTable = computed(() => ({
  key: { value: keyTextLocal.value, matchMode: props.filterState.keyMatchMode },
  type: { value: props.filterState.typeNames, matchMode: 'in' },
  references: { value: props.filterState.refsValue, matchMode: props.filterState.refsMatchMode }
}))

const debouncedSetKeyText = useDebounceFn((value) => {
  props.filterState.keyText = value
}, DEBOUNCE_KEY_MS)

function onFiltersUpdate(filters) {
  if (filters?.key) {
    keyTextLocal.value = filters.key.value || ''
    props.filterState.keyMatchMode = filters.key.matchMode || 'CONTAINS'
    debouncedSetKeyText(keyTextLocal.value)
  }
  if (filters?.type) {
    props.filterState.typeNames = Array.isArray(filters.type.value) ? filters.type.value : []
  }
  if (filters?.references) {
    props.filterState.refsValue = filters.references.value
    props.filterState.refsMatchMode = filters.references.matchMode || 'GREATER_THAN_OR_EQUAL_TO'
  }
}

function onKeyFilterInput(value) {
  keyTextLocal.value = value
  debouncedSetKeyText(value)
}

watch(() => props.filterState.keyText, (v) => {
  keyTextLocal.value = v || ''
})

function onPage(event) {
  emit('page', event)
}

function onSort(event) {
  emit('sort', event)
}

watch(selectedRow, (row) => {
  if (row && !props.value.includes(row)) {
    selectedRow.value = null
    return
  }
  selectedItem.value = row
  drawerVisible.value = !!row
})

watch(drawerVisible, (visible) => {
  if (!visible) selectedRow.value = null
})
</script>
