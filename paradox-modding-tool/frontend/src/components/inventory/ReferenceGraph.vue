<template>
  <Dialog :visible="true" modal header="Reference Graph" :style="{ width: '90vw', height: '85vh' }"
    @update:visible="$emit('close')" :closable="true">
    <div class="h-full flex flex-col">
      <!-- Controls -->
      <div class="mb-2 flex gap-2 items-center">
        <span class="text-sm text-gray-400">{{ nodes.length }} nodes, {{ links.length }} edges</span>
        <Button label="Reset View" size="small" severity="secondary" text @click="resetView" />
      </div>

      <!-- Chart -->
      <div class="flex-1 min-h-0">
        <v-chart ref="chart" :option="chartOption" autoresize class="w-full h-full" @click="onNodeClick" />
      </div>

      <!-- Legend -->
      <div class="mt-2 flex flex-wrap gap-2">
        <div v-for="cat in categories" :key="cat.name" class="flex items-center gap-1 text-xs">
          <span class="w-3 h-3 rounded-full" :style="{ backgroundColor: cat.itemStyle?.color || '#666' }"></span>
          <span>{{ cat.name }}</span>
        </div>
      </div>
    </div>
  </Dialog>
</template>

<!-- TODO: Need to open detail view when you double click on a node -->
<!-- TODO: Need to fix performance for super high node counts (19000+), maybe disable force/physics somehow? -->

<script>
import { use } from 'echarts/core'
import { GraphChart } from 'echarts/charts'
import { TooltipComponent, LegendComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import VChart from 'vue-echarts'
import Dialog from 'primevue/dialog'
import Button from 'primevue/button'

use([GraphChart, TooltipComponent, LegendComponent, CanvasRenderer])

// Color palette for object types
const TYPE_COLORS = [
  '#5470c6', '#91cc75', '#fac858', '#ee6666', '#73c0de',
  '#3ba272', '#fc8452', '#9a60b4', '#ea7ccc', '#48b8d0',
  '#ff9f7f', '#87cefa', '#da70d6', '#32cd32', '#6495ed'
]

export default {
  name: 'ReferenceGraph',
  components: { VChart, Dialog, Button },
  props: {
    inventory: {
      type: Object,
      default: () => ({})
    },
    /** Precomputed graph from Go backend (nodes + links). When set, used instead of computing from inventory. */
    graph: {
      type: Object,
      default: null
    },
    focusItem: {
      type: Object,
      default: null
    }
  },
  emits: ['close'],
  data() {
    return {
      highlightedNode: null
    }
  },
  computed: {
    types() {
      if (this.graph?.nodes?.length) {
        const typeSet = new Set(this.graph.nodes.map((n) => n.id.split(':')[0]))
        return [...typeSet].sort()
      }
      return Object.keys(this.inventory).sort()
    },
    categories() {
      return this.types.map((type, idx) => ({
        name: type,
        itemStyle: { color: TYPE_COLORS[idx % TYPE_COLORS.length] }
      }))
    },
    nodes() {
      if (this.graph?.nodes?.length) {
        return this.graph.nodes.map((n) => ({ ...n, itemData: null }))
      }
      const nodes = []
      const nodeMap = new Map()
      for (const [type, result] of Object.entries(this.inventory)) {
        const categoryIdx = this.types.indexOf(type)
        for (const item of result.items || []) {
          const nodeId = `${type}:${item.key}`
          if (!nodeMap.has(nodeId)) {
            const refCount = item.references?.length || 0
            nodes.push({
              id: nodeId,
              name: item.key,
              category: categoryIdx,
              symbolSize: Math.min(10 + refCount * 2, 40),
              value: refCount,
              itemData: item
            })
            nodeMap.set(nodeId, true)
          }
        }
      }
      return nodes
    },
    links() {
      if (this.graph?.links?.length) {
        return this.graph.links
      }
      const links = []
      const linkSet = new Set()
      for (const [type, result] of Object.entries(this.inventory)) {
        for (const item of result.items || []) {
          if (item.references) {
            for (const ref of item.references) {
              const sourceId = `${type}:${item.key}`
              const targetId = `${ref.targetType}:${ref.targetKey}`
              const linkKey = `${sourceId}->${targetId}`
              if (!linkSet.has(linkKey)) {
                links.push({ source: sourceId, target: targetId })
                linkSet.add(linkKey)
              }
            }
          }
        }
      }
      return links
    },
    chartOption() {
      return {
        tooltip: {
          trigger: 'item',
          formatter: (params) => {
            if (params.dataType === 'node') {
              const item = params.data.itemData
              const typeName = item?.type ?? this.categories[params.data.category]?.name ?? 'unknown'
              return `<strong>${params.data.name}</strong><br/>
                      Type: ${typeName}<br/>
                      References: ${params.data.value ?? 0}`
            }
            return ''
          }
        },
        legend: {
          data: this.categories.map(c => c.name),
          orient: 'vertical',
          right: 10,
          top: 20,
          textStyle: { color: '#aaa' }
        },
        series: [{
          type: 'graph',
          layout: 'force',
          data: this.nodes,
          links: this.links,
          categories: this.categories,
          roam: true,
          draggable: true,
          label: {
            show: true,
            position: 'right',
            fontSize: 10,
            color: '#ccc'
          },
          labelLayout: {
            hideOverlap: true
          },
          emphasis: {
            focus: 'adjacency',
            lineStyle: { width: 3 }
          },
          force: {
            repulsion: 200,
            edgeLength: [50, 150],
            gravity: 0.1
          },
          lineStyle: {
            color: 'source',
            opacity: 0.5,
            curveness: 0.1
          },
          edgeSymbol: ['none', 'arrow'],
          edgeSymbolSize: [0, 8]
        }]
      }
    }
  },
  mounted() {
    if (this.focusItem) {
      this.$nextTick(() => {
        this.focusOnNode(this.focusItem)
      })
    }
  },
  methods: {
    onNodeClick(params) {
      if (params.dataType === 'node') {
        this.highlightedNode = params.data
      }
    },
    focusOnNode(item) {
      // Find the node and center the view on it
      const nodeId = `${item.type}:${item.key}`
      const chart = this.$refs.chart
      if (chart) {
        chart.dispatchAction({
          type: 'highlight',
          seriesIndex: 0,
          name: item.key
        })
      }
    },
    resetView() {
      const chart = this.$refs.chart
      if (chart) {
        chart.dispatchAction({
          type: 'restore'
        })
      }
    }
  }
}
</script>
