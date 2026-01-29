<template>
  <div v-if="visible" class="fixed inset-0 z-60 flex flex-col bg-(--p-surface-900)" @click.self="$emit('close')">
    <div class="flex-1 flex flex-col min-h-0 m-2 bg-dark-panel rounded-xl border border-dark-border overflow-hidden"
      @click.stop>
      <!-- Header -->
      <div class="px-4 py-4 border-b border-dark-border bg-dark-panel/80">
        <div class="flex justify-between items-center gap-2 mb-3">
          <h2 class="text-lg font-semibold truncate">Diff Viewer</h2>
          <Button label="Close" @click="$emit('close')" />
        </div>
        <div class="flex flex-wrap items-center gap-2">
          <span class="text-sm text-gray-400">View:</span>
          <SelectButton v-model="viewMode"
            :options="[{ label: 'Unified', value: 'unified' }, { label: 'Side-by-side', value: 'sidebyside' }]" optionLabel="label"
            optionValue="value" />
          <InputText ref="searchInput" v-model="searchQuery" @input="performSearch" @keydown.enter.prevent="nextMatch"
            @keydown.shift.enter.prevent="prevMatch" placeholder="Search (Ctrl+F)" class="flex-1 min-w-0 px-3 py-2" />
          <template v-if="searchQuery">
            <span v-if="searchMatches.length" class="text-sm text-gray-400">{{ currentMatchIndex + 1 }}/{{
              searchMatches.length }}</span>
            <Button label="Clear" severity="secondary" @click="clearSearch" />
            <Button v-if="searchMatches.length" label="↑" @click="prevMatch" />
            <Button v-if="searchMatches.length" label="↓" @click="nextMatch" />
          </template>
        </div>
      </div>

      <!-- Side-by-side View -->
      <template v-if="viewMode === 'sidebyside' && lines.length">
        <div ref="sbsScroll" class="flex-1 overflow-y-auto overflow-x-hidden bg-dark-panel">
          <!-- Header band -->
          <div class="border-b border-dark-border/50 bg-diff-header">
            <div v-for="h in headers" :key="h.idx" class="font-mono text-sm leading-6 min-h-6 px-3"
              style="width:max-content">
              <span :class="h.content.startsWith('---') ? 'text-red-500/90' : 'text-accent-success'"
                class="font-semibold mr-1 select-none">
                {{ h.content.startsWith('---') ? '---' : '+++' }}
              </span>
              <span class="text-slate-400 mr-1">{{ h.content.startsWith('---') ? 'Base:' : 'Compare:' }}</span>
              <span class="whitespace-pre">{{ h.content.slice(3).trim() }}</span>
            </div>
          </div>
          <!-- Columns -->
          <div class="flex">
            <div class="flex-1 overflow-hidden border-r border-dark-border">
              <div ref="leftCol" class="font-mono text-sm leading-6"
                :style="{ width: 'max-content', minWidth: '100%', transform: `translateX(-${sbsScroll.left}px)` }">
                <div v-for="r in rows" :key="'L' + r.n" :class="['flex min-h-6 border-l-[3px]', rowClass(r.left)]">
                  <div
                    class="flex min-w-16 border-r border-dark-border/50 bg-dark-input/50 px-2 justify-end select-none">
                    <span class="min-w-10 text-right tabular-nums text-slate-400">{{ r.n }}</span>
                  </div>
                  <div class="px-3">
                    <span v-if="r.left?.type === 'remove'" class="mr-2 font-semibold select-none text-red-500/90">-</span>
                    <span class="whitespace-pre">{{ r.left?.content || '\u00a0' }}</span>
                  </div>
                </div>
              </div>
            </div>
            <div class="flex-1 overflow-hidden">
              <div ref="rightCol" class="font-mono text-sm leading-6"
                :style="{ width: 'max-content', minWidth: '100%', transform: `translateX(-${sbsScroll.right}px)` }">
                <div v-for="r in rows" :key="'R' + r.n"
                  :class="['flex min-h-6 border-l-[3px]', rowClass(r.right, 'right')]">
                  <div
                    class="flex min-w-16 border-r border-dark-border/50 bg-dark-input/50 px-2 justify-end select-none">
                    <span class="min-w-10 text-right tabular-nums text-slate-400">{{ r.n }}</span>
                  </div>
                  <div class="px-3">
                    <span v-if="r.right?.type === 'add'"
                      class="mr-2 font-semibold select-none text-emerald-500/90">+</span>
                    <span class="whitespace-pre">{{ r.right?.content || '\u00a0' }}</span>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </div>
        <!-- Scrollbars -->
        <div class="flex border-t border-dark-border/60 h-4">
          <div class="flex-1 overflow-x-auto border-r border-dark-border/50"
            @scroll="sbsScroll.left = $event.target.scrollLeft">
            <div :style="{ width: (leftColWidth || 0) + 'px', height: '1px' }" />
          </div>
          <div class="flex-1 overflow-x-auto" @scroll="sbsScroll.right = $event.target.scrollLeft">
            <div :style="{ width: (rightColWidth || 0) + 'px', height: '1px' }" />
          </div>
        </div>
      </template>

      <!-- Unified View -->
      <template v-else>
        <div ref="unifiedScroll" class="flex-1 overflow-auto bg-dark-panel">
          <div v-if="!lines.length" class="p-8 text-center text-gray-400">No differences found.</div>
          <div v-else class="font-mono text-sm leading-6" style="min-width:100%;width:max-content">
            <div v-for="(l, i) in lines" :key="i"
              :class="['flex min-h-6 border-l-[3px]', lineClass(l.type), isMatch(i) && 'bg-yellow-500/20']">
              <div
                class="flex min-w-32 border-r border-dark-border/50 bg-dark-input/50 px-3 justify-end gap-4 select-none">
                <span class="min-w-10 text-right tabular-nums"
                  :class="l.oldLineNum ? 'text-slate-400' : 'text-gray-500'">{{ l.oldLineNum ?? '' }}</span>
                <span class="min-w-10 text-right tabular-nums"
                  :class="l.newLineNum ? 'text-slate-400' : 'text-gray-500'">{{ l.newLineNum ?? '' }}</span>
              </div>
              <div class="px-3">
                <template v-if="l.content?.startsWith('---') || l.content?.startsWith('+++')">
                  <span :class="l.content.startsWith('---') ? 'text-red-500/90' : 'text-accent-success'"
                    class="mr-2 font-semibold select-none">
                    {{ l.content.slice(0, 3) }}
                  </span>
                  <span class="text-slate-400 mr-1">{{ l.content.startsWith('---') ? 'Base:' : 'Compare:' }}</span>
                  <span class="whitespace-pre">{{ l.content.slice(3).trim() }}</span>
                </template>
                <template v-else>
                  <span v-if="l.type === 'add'" class="mr-2 font-semibold select-none text-emerald-500/90">+</span>
                  <span v-else-if="l.type === 'remove'" class="mr-2 font-semibold select-none text-red-500/90">-</span>
                  <span class="whitespace-pre">{{ l.content }}</span>
                </template>
              </div>
            </div>
          </div>
        </div>
      </template>
    </div>
  </div>
</template>

<script>
import Button from 'primevue/button'
import InputText from 'primevue/inputtext'
import SelectButton from 'primevue/selectbutton'

export default {
  name: 'DiffViewer',
  components: { Button, InputText, SelectButton },
  props: { visible: Boolean, lines: { type: Array, default: () => [] }, loading: Boolean },
  data: () => ({
    searchQuery: '', searchMatches: [], currentMatchIndex: -1, viewMode: 'unified',
    sbsScroll: { left: 0, right: 0 }, leftColWidth: 0, rightColWidth: 0
  }),
  computed: {
    headers() { return this.lines.filter(l => l.type === 'header' && l.content?.trim()) },
    rows() {
      const old = {}, neu = {}
      let maxO = 0, maxN = 0
      for (const l of this.lines) {
        if (l.type === 'header' || l.type === 'other') continue
        if (l.oldLineNum) { old[l.oldLineNum] = l; maxO = Math.max(maxO, l.oldLineNum) }
        if (l.newLineNum) { neu[l.newLineNum] = l; maxN = Math.max(maxN, l.newLineNum) }
      }
      const r = []
      for (let i = 1; i <= Math.max(maxO, maxN); i++) {
        if (old[i] || neu[i]) r.push({ n: i, left: old[i], right: neu[i] })
      }
      return r
    }
  },
  watch: {
    visible(v) { document.body.style.overflow = v ? 'hidden' : '' },
    lines() { if (this.searchQuery) this.performSearch() }
  },
  mounted() { window.addEventListener('keydown', this.onKey) },
  beforeUnmount() { document.body.style.overflow = ''; window.removeEventListener('keydown', this.onKey) },
  updated() {
    this.leftColWidth = this.$refs.leftCol?.scrollWidth
    this.rightColWidth = this.$refs.rightCol?.scrollWidth
  },
  methods: {
    rowClass(l, side = 'left') {
      if (!l) return side === 'left' ? 'bg-dark-input/30 border-l-transparent' : 'bg-dark-input/20 border-l-transparent'
      if (l.type === 'remove') return 'bg-diff-remove border-l-red-500/50'
      if (l.type === 'add') return 'bg-diff-add border-l-emerald-500/50'
      return 'bg-diff-context border-l-transparent'
    },
    lineClass(t) {
      return {
        add: 'bg-diff-add border-l-emerald-500/50', remove: 'bg-diff-remove border-l-red-500/50',
        header: 'bg-diff-header border-l-transparent', context: 'bg-diff-context border-l-transparent'
      }[t] || 'border-l-transparent'
    },
    isMatch(i) { return this.searchMatches[this.currentMatchIndex] === i },
    onKey(e) { if ((e.ctrlKey || e.metaKey) && e.key === 'f') { e.preventDefault(); this.$refs.searchInput?.focus() } },
    performSearch() {
      const q = this.searchQuery.trim().toLowerCase()
      this.searchMatches = q ? this.lines.map((l, i) => l.type !== 'header' && l.type !== 'other' && l.content?.toLowerCase().includes(q) ? i : -1).filter(i => i >= 0) : []
      this.currentMatchIndex = this.searchMatches.length ? 0 : -1
      this.scrollToMatch()
    },
    nextMatch() { if (this.searchMatches.length) { this.currentMatchIndex = (this.currentMatchIndex + 1) % this.searchMatches.length; this.scrollToMatch() } },
    prevMatch() { if (this.searchMatches.length) { this.currentMatchIndex = this.currentMatchIndex <= 0 ? this.searchMatches.length - 1 : this.currentMatchIndex - 1; this.scrollToMatch() } },
    scrollToMatch() { this.$nextTick(() => this.$el.querySelector('.bg-yellow-500\\/20')?.scrollIntoView({ behavior: 'smooth', block: 'center' })) },
    clearSearch() { this.searchQuery = ''; this.searchMatches = []; this.currentMatchIndex = -1 }
  }
}
</script>
