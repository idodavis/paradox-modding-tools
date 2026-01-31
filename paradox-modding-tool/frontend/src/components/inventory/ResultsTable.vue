<template>
  <div class="flex flex-col rounded-xl border border-dark-border overflow-hidden overscroll-none">
    <!-- DataTable: :filters (PrimeVue shape) so filter icon overlay works; @update:filters syncs overlay choices to filterState. No v-model:filters to avoid recursion. -->
    <DataTable :value="filteredData" :filters="filtersForDataTable" filterDisplay="row" paginator :rows="10"
      :loading="loading" selectionMode="single" v-model:selection="selectedRow" dataKey="uniqueId"
      @rowSelect="onRowSelect" @update:filters="onFiltersUpdate" stripedRows>
      <template #empty> No objects found. </template>
      <template #loading> Loading inventory data. Please wait. </template>
      <Column field="key" header="Key" sortable dataType="text" showFilterMenu showFilterMatchModes
        :filterMatchModeOptions="keyMatchModeOptions" class="min-w-50">
        <template #body="{ data }">
          <span class="font-mono text-sm">{{ data.key }}</span>
        </template>
        <template #filter>
          <div class="flex flex-col gap-1">
            <InputText :modelValue="filterState.keyText" @update:modelValue="filterState.keyText = $event" type="text"
              placeholder="Search by key" class="w-full min-w-0" />
          </div>
        </template>
      </Column>
      <Column field="type" header="Type" sortable showFilterMenu class="w-37">
        <template #body="{ data }">
          <Tag :value="data.type" severity="success" />
        </template>
        <template #filter>
          <MultiSelect :modelValue="filterState.typeNames" @update:modelValue="filterState.typeNames = $event"
            :options="filterTypeOptions" optionLabel="type" optionValue="type" placeholder="All types"
            class="w-full min-w-0">
            <template #option="slotProps">
              <div class="flex items-center gap-2">
                <Tag :value="slotProps.option.type" severity="success" />
              </div>
            </template>
          </MultiSelect>
        </template>
      </Column>
      <Column field="filePath" header="File" sortable class="min-w-62">
        <template #body="{ data }">
          <span class="text-sm text-gray-300 truncate" :title="data.filePath">{{ shortenPath(data.filePath) }}</span>
        </template>
      </Column>
      <Column header="Lines" class="w-25">
        <template #body="{ data }">
          <span class="text-sm text-gray-400">{{ data.lineStart }}-{{ data.lineEnd }}</span>
        </template>
      </Column>
      <Column field="references" header="Refs" sortable dataType="numeric" showFilterMenu showFilterMatchModes
        :filterMatchModeOptions="refsMatchModeOptions" class="w-20">
        <template #body="{ data }">
          <span class="text-sm text-blue-400">
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
  </div>
</template>

<script setup>
import { ref, computed, watch } from 'vue'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import InputText from 'primevue/inputtext'
import InputNumber from 'primevue/inputnumber'
import MultiSelect from 'primevue/multiselect'
import Select from 'primevue/select'
import Tag from 'primevue/tag'
import { FilterMatchMode } from '@primevue/core/api'
import { applyInventoryFilter } from '../../utils/inventory.js'

const keyMatchModeOptions = [
  { label: 'Contains', value: FilterMatchMode.CONTAINS },
  { label: 'Starts with', value: FilterMatchMode.STARTS_WITH },
  { label: 'Ends with', value: FilterMatchMode.ENDS_WITH },
  { label: 'Equals', value: FilterMatchMode.EQUALS },
  { label: 'Not contains', value: FilterMatchMode.NOT_CONTAINS },
  { label: 'Not equals', value: FilterMatchMode.NOT_EQUALS }
]

const refsMatchModeOptions = [
  { label: '≥', value: FilterMatchMode.GREATER_THAN_OR_EQUAL_TO },
  { label: '>', value: FilterMatchMode.GREATER_THAN },
  { label: '=', value: FilterMatchMode.EQUALS },
  { label: '≠', value: FilterMatchMode.NOT_EQUALS },
  { label: '≤', value: FilterMatchMode.LESS_THAN_OR_EQUAL_TO },
  { label: '<', value: FilterMatchMode.LESS_THAN }
]

const emit = defineEmits(['select', 'filter-change'])

const props = defineProps({
  inventory: {
    type: Object,
    required: true
  },
  loading: {
    type: Boolean,
    default: false
  },
  /** Reactive filter state (owned by parent). Mutations here update table and export. */
  filterState: {
    type: Object,
    required: true
  }
})

const selectedRow = ref(null)
const filteredData = ref([])

/** PrimeVue-shaped filters so the filter icon overlay has data; derived from filterState. */
const filtersForDataTable = computed(() => ({
  key: { value: props.filterState.keyText ?? null, matchMode: props.filterState.keyMatchMode || 'contains' },
  type: { value: props.filterState.typeNames ?? [], matchMode: 'in' },
  references: { value: props.filterState.refsValue ?? null, matchMode: props.filterState.refsMatchMode || 'gte' }
}))

/** When user picks match mode (or clears) in the filter overlay, sync back to filterState. */
function onFiltersUpdate(filters) {
  if (!filters) return
  if (filters.key) {
    props.filterState.keyText = filters.key.value ?? ''
    props.filterState.keyMatchMode = filters.key.matchMode || 'contains'
  }
  if (filters.type) {
    props.filterState.typeNames = Array.isArray(filters.type.value) ? filters.type.value : []
  }
  if (filters.references) {
    props.filterState.refsValue = filters.references.value ?? null
    props.filterState.refsMatchMode = filters.references.matchMode || 'gte'
  }
  emit('filter-change', filters)
}

function applyFilters() {
  const { flat } = applyInventoryFilter(props.inventory, props.filterState)
  filteredData.value = flat
}

watch(
  [() => props.inventory, () => props.filterState],
  () => {
    applyFilters()
  },
  { immediate: true, deep: true }
)

const filterTypeOptions = computed(() => {
  return Object.keys(props.inventory || {}).sort().map((type) => ({ type }))
})

function onRowSelect(event) {
  emit('select', event.data)
}

function shortenPath(path) {
  if (!path) return ''
  const parts = path.split(/[/\\]/)
  if (parts.length > 3) {
    return '.../' + parts.slice(-3).join('/')
  }
  return path
}
</script>
