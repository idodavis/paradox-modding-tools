<template>
  <div class="flex flex-col rounded-xl border border-dark-border overflow-hidden overscroll-none">
    <DataTable
      :value="value"
      :totalRecords="totalRecords"
      :lazy="true"
      :first="(currentPage - 1) * pageSize"
      :rows="pageSize"
      :filters="filtersForDataTable"
      filterDisplay="row"
      paginator
      :rowsPerPageOptions="[10, 25, 50, 100]"
      :loading="loading"
      selectionMode="single"
      v-model:selection="selectedRow"
      dataKey="uniqueId"
      :sortField="sortField"
      :sortOrder="sortOrder"
      @rowSelect="onRowSelect"
      @update:filters="onFiltersUpdate"
      @page="onPage"
      @sort="onSort"
      stripedRows
    >
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
            <InputText :modelValue="keyTextLocal" @update:modelValue="onKeyFilterInput($event)" type="text"
              placeholder="Search by key" class="w-full min-w-0" />
          </div>
        </template>
      </Column>
      <Column field="type" header="Type" sortable showFilterMenu class="w-37 min-w-0">
        <template #body="{ data }">
          <Tag :value="data.type" severity="success" />
        </template>
        <template #filter>
          <MultiSelect
            :modelValue="filterState.typeNames"
            @update:modelValue="filterState.typeNames = $event"
            :options="filterTypeOptions"
            optionLabel="type"
            optionValue="type"
            placeholder="All types"
            :maxSelectedLabels="3"
            filter
            filterPlaceholder="Type to filter..."
            class="w-full min-w-0 [&_.p-multiselect-label]:min-h-0 [&_.p-multiselect-trigger]:overflow-hidden [&_.p-multiselect-trigger]:max-h-10"
            panelClass="max-h-56 overflow-auto"
          >
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
  </div>
</template>

<script setup>
import { ref, computed, watch } from 'vue'
import { FilterMatchMode } from '@primevue/core/api'
import ProgressSpinner from 'primevue/progressspinner'
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

const emit = defineEmits(['select', 'filter-change', 'sort', 'page'])

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
    default: 'key'
  },
  sortOrder: {
    type: Number,
    default: 1
  },
  currentPage: {
    type: Number,
    default: 1
  },
  pageSize: {
    type: Number,
    default: 25
  }
})

const selectedRow = ref(null)
const keyTextLocal = ref('')

const filterTypeOptions = computed(() =>
  (props.availableTypeNames || []).slice().sort().map((type) => ({ type }))
)

const filtersForDataTable = computed(() => ({
  key: { value: keyTextLocal.value ?? props.filterState.keyText ?? null, matchMode: props.filterState.keyMatchMode || 'contains' },
  type: { value: props.filterState.typeNames ?? [], matchMode: 'in' },
  references: { value: props.filterState.refsValue ?? null, matchMode: props.filterState.refsMatchMode || 'gte' }
}))

function onFiltersUpdate(filters) {
  if (!filters) return
  if (filters.key) {
    const v = filters.key.value ?? ''
    keyTextLocal.value = v
    props.filterState.keyText = v
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

function onKeyFilterInput(value) {
  keyTextLocal.value = value
  debouncedSetKeyText(value)
}

const debouncedSetKeyText = useDebounceFn((value) => {
  props.filterState.keyText = value
}, DEBOUNCE_KEY_MS)

function onPage(event) {
  emit('page', event)
}

function onSort(event) {
  emit('sort', event)
}

watch(
  () => props.filterState.keyText,
  (v) => { keyTextLocal.value = v ?? '' }
)

function onRowSelect(event) {
  emit('select', event.data)
}
</script>
