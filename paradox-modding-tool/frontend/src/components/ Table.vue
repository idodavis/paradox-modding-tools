<template>
  <div
    class="flex flex-col min-h-0 min-w-0 w-full max-w-full bg-dark-panel/60 backdrop-blur-sm rounded-xl p-4 sm:p-6 border border-dark-border/50"
    :class="panelClass" :style="panelStyle">
    <div class="flex items-center justify-between gap-3 mb-3 flex-shrink-0">
      <h3 class="text-base sm:text-lg font-semibold">{{ title }}</h3>
      <slot name="actions" />
    </div>
    <DataTable class="flex-1 min-h-0 text-sm" :value="value" :dataKey="dataKey" :scrollable="true" scrollHeight="flex"
      stripedRows :tableStyle="{ minWidth: '100%' }">
      <slot />
    </DataTable>
  </div>
</template>

<script>
import DataTable from 'primevue/datatable'

export default {
  name: 'DataTablePanel',
  components: { DataTable },
  props: {
    title: { type: String, required: true },
    value: { type: Array, default: () => [] },
    dataKey: { type: String, default: undefined },
    panelClass: { type: [String, Array, Object], default: '' },
    panelStyle: { type: [String, Object], default: undefined }
  }
}
</script>
