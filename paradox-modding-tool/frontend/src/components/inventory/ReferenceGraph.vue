<template>
  <div class="flex flex-col h-full min-h-0 overflow-hidden bg-dark-bg rounded-lg">
    <div class="flex flex-wrap items-center justify-between gap-2 p-3 border-b border-dark-border">
      <div class="flex flex-wrap items-center gap-2">
        <span class="text-sm text-(--p-surface-400)">{{ nodes.length }} nodes, {{ links.length }} edges</span>
        <span class="text-(--p-surface-500) text-xs">(this view)</span>
        <span v-if="cappedMessage" class="text-(--p-surface-500) text-xs">{{ cappedMessage }}</span>
        <Button v-if="totalRefs > REF_PAGE_SIZE" label="Next 100 refs" size="small" severity="secondary" outlined
          @click="emit('next-refs')" />
        <Button v-if="totalReferrers > REF_PAGE_SIZE" label="Next 100 referrers" size="small" severity="secondary"
          outlined @click="emit('next-referrers')" />
      </div>
      <span class="text-xs text-(--p-surface-500)">Scroll to zoom · Drag to pan · Click node for details</span>
    </div>

    <!-- Chart -->
    <v-chart ref="chartRef" :option="chartOption" @click="onNodeClick" />
  </div>
</template>

<script setup>
import { ref, computed, onBeforeUnmount } from 'vue'
import { use } from 'echarts/core'
import { GraphChart } from 'echarts/charts'
import { TooltipComponent, LegendComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import VChart from 'vue-echarts'

use([GraphChart, TooltipComponent, LegendComponent, CanvasRenderer])

const REF_PAGE_SIZE = 100

const props = defineProps({
  /** Nodes and links (built in frontend from inventory items). */
  graphData: {
    type: Object,
    default: () => ({ nodes: [], links: [], totalRefs: 0, totalReferrers: 0 })
  },
  focusItem: {
    type: Object,
    required: true
  },
  /** Current refs window offset (for capped message). */
  refsOffset: {
    type: Number,
    default: 0
  },
  /** Current referrers window offset (for capped message). */
  referrersOffset: {
    type: Number,
    default: 0
  }
})

const emit = defineEmits(['open-item', 'next-refs', 'next-referrers'])

const chartRef = ref(null)

const nodes = computed(() => props.graphData?.nodes ?? [])
const links = computed(() => props.graphData?.links ?? [])
const totalRefs = computed(() => props.graphData?.totalRefs ?? 0)
const totalReferrers = computed(() => props.graphData?.totalReferrers ?? 0)

const types = computed(() => {
  const typeSet = new Set(nodes.value.map((n) => n.id?.split(':')[0]).filter(Boolean))
  return [...typeSet].sort()
})

const cappedMessage = computed(() => {
  if (!props.focusItem) return ''
  const parts = []
  if (totalRefs.value > REF_PAGE_SIZE) {
    const start = props.refsOffset + 1
    const end = Math.min(props.refsOffset + REF_PAGE_SIZE, totalRefs.value)
    parts.push(`Refs ${start}–${end} of ${totalRefs.value}`)
  }
  if (totalReferrers.value > REF_PAGE_SIZE) {
    const start = props.referrersOffset + 1
    const end = Math.min(props.referrersOffset + REF_PAGE_SIZE, totalReferrers.value)
    parts.push(`Referrers ${start}–${end} of ${totalReferrers.value}`)
  }
  return parts.length ? parts.join(' · ') : ''
})

const categories = computed(() =>
  types.value.map((type) => ({ name: type }))
)

const nodeCount = computed(() => nodes.value.length)
const chartOption = computed(() => {
  const count = nodeCount.value
  const isLarge = count > 150
  return {
    tooltip: {
      trigger: 'item',
      formatter: (params) => {
        if (params.dataType === 'node') {
          const item = params.data.itemData
          const typeName = item?.type ?? categories.value[params.data.category]?.name ?? 'unknown'
          return `<strong>${params.data.name}</strong><br/>
                  Type: ${typeName}<br/>
                  References: ${params.data.value ?? 0}`
        }
        return ''
      }
    },
    legend: {
      data: categories.value.map((c) => c.name),
      orient: 'vertical',
      right: 10,
      top: 20,
      textStyle: { color: '#aaa' }
    },
    series: [{
      type: 'graph',
      layout: 'circular',
      data: nodes.value,
      links: links.value,
      categories: categories.value,
      roam: true,
      circular: { rotateLabel: true },
      label: {
        show: true,
        position: 'right',
        fontSize: 12,
        color: '#fff',
        formatter: (params) => {
          const d = params.data
          if (d?.itemData?.key) return d.itemData.key
          return d?.name
        }
      },
      labelLayout: { hideOverlap: !isLarge },
      emphasis: isLarge ? { lineStyle: { width: 2 } } : { focus: 'adjacency', lineStyle: { width: 3 } },
      lineStyle: { color: 'source', curveness: 0.1 },
    }],
  }
})

function onNodeClick(params) {
  if (params?.componentType !== 'series' || params?.dataType !== 'node' || !params?.data) return
  const item = params.data.itemData
  if (item) emit('open-item', item)
}

onBeforeUnmount(() => {
  const chart = chartRef.value?.getChart?.()
  if (chart) chart.off('click', onNodeClick)
})
</script>
