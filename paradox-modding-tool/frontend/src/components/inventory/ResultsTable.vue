<template>
  <div class="flex flex-col rounded-xl border border-dark-border overflow-hidden min-h-100 h-full">
    <!-- Filter Bar -->
    <div class="p-3 border-b border-dark-border bg-dark-panel/50">
      <div class="flex flex-wrap gap-2 items-center mb-2">
        <InputText v-model="searchText" placeholder="Search by key..." class="w-64" @input="onSearchChange" />
        <span class="text-sm text-gray-400">{{ filteredItems.length }} items</span>
      </div>
      <!-- Type Filter Chips -->
      <div class="flex flex-wrap gap-1">
        <Tag v-for="type in availableTypes" :key="type" :value="type"
          :severity="isTypeSelected(type) ? 'info' : 'secondary'" class="cursor-pointer" @click="toggleType(type)" />
      </div>
    </div>

    <!-- Data Table: virtual scroll + overscroll-none so inner scroll doesn't bounce at boundaries -->
    <DataTable :value="filteredItems" :virtualScrollerOptions="{ itemSize: 40 }" scrollable scrollHeight="flex"
      selectionMode="single" v-model:selection="selectedRow" dataKey="uniqueId" @rowSelect="onRowSelect"
      class="flex-1 results-table" stripedRows>
      <Column field="key" header="Key" sortable style="min-width: 12.5rem">
        <template #body="{ data }">
          <span class="font-mono text-sm">{{ data.key }}</span>
        </template>
      </Column>
      <Column field="type" header="Type" sortable style="width: 9.375rem">
        <template #body="{ data }">
          <Tag :value="data.type" severity="info" />
        </template>
      </Column>
      <Column field="filePath" header="File" sortable style="min-width: 15.625rem">
        <template #body="{ data }">
          <span class="text-sm text-gray-300 truncate" :title="data.filePath">{{ shortenPath(data.filePath) }}</span>
        </template>
      </Column>
      <Column header="Lines" style="width: 6.25rem">
        <template #body="{ data }">
          <span class="text-sm text-gray-400">{{ data.lineStart }}-{{ data.lineEnd }}</span>
        </template>
      </Column>
      <Column header="Refs" style="width: 5rem">
        <template #body="{ data }">
          <span v-if="data.references && data.references.length > 0" class="text-sm text-blue-400">
            {{ data.references.length }}
          </span>
          <span v-else class="text-sm text-gray-500">-</span>
        </template>
      </Column>
    </DataTable>
  </div>
</template>

<script>
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import InputText from 'primevue/inputtext'
import Tag from 'primevue/tag'
import { filterInventoryItems } from '../../utils/inventory.js'

export default {
  name: 'ResultsTable',
  components: { DataTable, Column, InputText, Tag },
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
  emits: ['select', 'update:filter-text', 'update:filter-types'],
  data() {
    return {
      searchText: this.filterText,
      selectedRow: null
    }
  },
  computed: {
    availableTypes() {
      return Object.keys(this.inventory).sort()
    },
    filteredItems() {
      return filterInventoryItems(this.inventory, {
        filterText: this.searchText,
        filterTypes: this.filterTypes,
      })
    }
  },
  watch: {
    filterText(newVal) {
      this.searchText = newVal
    }
  },
  methods: {
    isTypeSelected(type) {
      return this.filterTypes.length === 0 || this.filterTypes.includes(type)
    },
    toggleType(type) {
      let newTypes
      if (this.filterTypes.length === 0) {
        // First click: show only this type
        newTypes = [type]
      } else if (this.filterTypes.includes(type)) {
        // Remove type
        newTypes = this.filterTypes.filter(t => t !== type)
        if (newTypes.length === 0) {
          // If all removed, show all
          newTypes = []
        }
      } else {
        // Add type
        newTypes = [...this.filterTypes, type]
      }
      this.$emit('update:filter-types', newTypes)
    },
    onSearchChange() {
      this.$emit('update:filter-text', this.searchText)
    },
    onRowSelect(event) {
      this.$emit('select', event.data)
    },
    shortenPath(path) {
      if (!path) return ''
      const parts = path.split(/[/\\]/)
      if (parts.length > 3) {
        return '.../' + parts.slice(-3).join('/')
      }
      return path
    }
  }
}
</script>

<style scoped>
/* No bounce when overscrolling at table top/bottom (PrimeVue scroll is inside .p-datatable-wrapper) */
.results-table :deep(.p-datatable-wrapper) {
  overscroll-behavior: none;
}
</style>
