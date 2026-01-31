<template>
  <div class="flex flex-col h-full min-h-0 overflow-hidden bg-dark-bg rounded-lg">
    <div class="flex justify-between p-3 border-b border-dark-border flex-wrap">
      <span class="text-sm text-gray-400">{{ nodes.length }} nodes, {{ links.length }} edges</span>
      <span class="text-xs text-gray-500">Scroll to zoom · Drag to pan · Click node for details</span>
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

const props = defineProps({
  inventory: {
    type: Object,
    default: () => ({})
  },
  focusItem: {
    type: Object,
    required: true
  }
})

const emit = defineEmits(['open-item'])

const chartRef = ref(null)

function getItem(typeName, key) {
  const result = props.inventory[typeName]
  if (!result?.items) return null
  return result.items.find((i) => i.key === key) || null
}

const nodes = computed(() => {
  const focus = props.focusItem
  if (!focus) return []
  const nodeMap = new Map()
  const typeSet = new Set()
  typeSet.add(focus.type)

  const addNode = (item, typeName) => {
    const nodeId = `${typeName}:${item.key}`
    if (nodeMap.has(nodeId)) return
    nodeMap.set(nodeId, true)
    typeSet.add(typeName)
    const refCount = item.references?.length || 0
    return {
      id: nodeId,
      name: item.key,
      category: 0,
      symbolSize: Math.min(14 + refCount * 2, 60),
      value: refCount,
      itemData: item
    }
  }

  const result = []
  result.push(addNode(focus, focus.type))
  for (const ref of focus.references || []) {
    const item = getItem(ref.targetType, ref.targetKey)
    if (item) result.push(addNode(item, ref.targetType))
  }
  for (const typeName of Object.keys(props.inventory)) {
    for (const item of props.inventory[typeName].items || []) {
      const hasRefFromFocus = (item.references || []).some(
        (r) => r.targetKey === focus.key && r.targetType === focus.type
      )
      if (hasRefFromFocus) result.push(addNode(item, typeName))
    }
  }

  const typeList = [...typeSet].sort()
  const typeIdx = (t) => typeList.indexOf(t)
  result.forEach((n) => {
    n.category = typeIdx(n.id.split(':')[0])
  })
  return result
})

const links = computed(() => {
  const focus = props.focusItem
  if (!focus) return []
  const linkSet = new Set()
  const result = []
  const focusId = `${focus.type}:${focus.key}`

  for (const ref of focus.references || []) {
    const sourceId = `${ref.targetType}:${ref.targetKey}`
    const key = `${sourceId}->${focusId}`
    if (!linkSet.has(key)) {
      linkSet.add(key)
      result.push({ source: sourceId, target: focusId })
    }
  }

  for (const typeName of Object.keys(props.inventory)) {
    for (const item of props.inventory[typeName].items || []) {
      const hasRefFromFocus = (item.references || []).some(
        (r) => r.targetKey === focus.key && r.targetType === focus.type
      )
      if (hasRefFromFocus) {
        const targetId = `${typeName}:${item.key}`
        const key = `${focusId}->${targetId}`
        if (!linkSet.has(key)) {
          linkSet.add(key)
          result.push({ source: focusId, target: targetId })
        }
      }
    }
  }
  return result
})

const types = computed(() => {
  const typeSet = new Set(nodes.value.map((n) => n.id.split(':')[0]))
  return [...typeSet].sort()
})

const categories = computed(() =>
  types.value.map((type) => ({ name: type }))
)

const chartOption = computed(() => ({
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
    labelLayout: { hideOverlap: true },
    emphasis: { focus: 'adjacency', lineStyle: { width: 3 } },
    lineStyle: { color: 'source', curveness: 0.1 },
  }],
}))

function onNodeClick(params) {
  if (params?.componentType !== 'series' || params?.dataType !== 'node' || !params?.data) return
  let item = params.data.itemData
  if (!item && params.data.name) {
    const idx = params.data.name.indexOf(':')
    if (idx > 0) {
      const typeName = params.data.name.slice(0, idx)
      const key = params.data.name.slice(idx + 1)
      item = getItem(typeName, key)
    }
  }
  if (item) emit('open-item', item)
}

onBeforeUnmount(() => {
  const chart = chartRef.value?.getChart?.()
  if (chart) chart.off('click', onNodeClick)
})
</script>
