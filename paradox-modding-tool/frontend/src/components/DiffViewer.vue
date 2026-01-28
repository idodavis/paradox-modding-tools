<template>
  <div v-if="visible" class="fixed inset-0 z-[60] flex flex-col bg-dark-bg"
    @click.self="$emit('close')" style="pointer-events: auto;">
    <div class="flex-1 flex flex-col min-h-0 min-w-0 m-2 sm:m-4 bg-dark-panel rounded-xl shadow-material-lg border border-dark-border overflow-hidden"
      @click.stop style="pointer-events: auto;">
      <!-- Header -->
      <div class="flex flex-col px-4 sm:px-6 py-4 border-b border-dark-border bg-dark-panel/80 flex-shrink-0">
        <div class="flex justify-between items-center gap-2 mb-3">
          <h2 class="text-lg sm:text-xl font-semibold truncate min-w-0">Diff Viewer</h2>
          <button @click="$emit('close')" class="btn-primary">Close</button>
        </div>
        <div class="flex flex-wrap items-center gap-2">
          <div class="inline-flex overflow-hidden rounded-lg border border-dark-border bg-dark-input/60" role="group" aria-label="View mode">
            <button type="button"
              class="min-h-[2.25rem] border-r border-dark-border px-3 py-1.5 text-[0.8125rem] transition-colors last:border-r-0"
              :class="viewMode === 'unified' ? 'bg-btn-primary/50 text-white hover:bg-btn-primary/65' : 'bg-transparent text-slate-400 hover:bg-dark-border/40 hover:text-slate-200'"
              @click="viewMode = 'unified'">Unified</button>
            <button type="button"
              class="min-h-[2.25rem] border-r border-dark-border px-3 py-1.5 text-[0.8125rem] transition-colors last:border-r-0"
              :class="viewMode === 'sidebyside' ? 'bg-btn-primary/50 text-white hover:bg-btn-primary/65' : 'bg-transparent text-slate-400 hover:bg-dark-border/40 hover:text-slate-200'"
              @click="viewMode = 'sidebyside'">Side-by-side</button>
          </div>
          <input ref="searchInput" v-model="searchQuery" @input="performSearch" @keydown.enter.prevent="nextMatch"
            @keydown.shift.enter.prevent="prevMatch" type="text" placeholder="Search (Ctrl+F)"
            class="flex-1 min-w-0 px-3 py-2 bg-dark-input/80 border border-dark-border rounded-lg text-gray-200 placeholder:text-gray-400 focus:outline-none focus:ring-2 focus:ring-btn-primary/50" />
          <span v-if="searchQuery && searchMatches.length > 0" class="text-sm text-gray-400 whitespace-nowrap">{{ currentMatchIndex + 1 }} / {{ searchMatches.length }}</span>
          <button v-if="searchQuery" @click="clearSearch" class="btn-secondary">Clear</button>
          <button v-if="searchQuery && searchMatches.length > 0" @click="prevMatch" class="btn-secondary">↑ Prev</button>
          <button v-if="searchQuery && searchMatches.length > 0" @click="nextMatch" class="btn-secondary">Next ↓</button>
        </div>
      </div>
      <!-- Content -->
      <div ref="contentWrap" class="flex-1 min-h-0 flex flex-col">
        <!-- Side-by-side: vertical scroll in content; horizontal bars in fixed strip so always visible -->
        <template v-if="viewMode === 'sidebyside' && !loading && lines.length > 0">
          <div class="flex-1 min-h-0 flex flex-col">
            <div ref="sideBySideScroll" class="flex-1 min-h-0 overflow-y-auto overflow-x-hidden bg-dark-panel">
              <div class="flex" :style="{ height: totalHeight + 'px' }">
                <div class="flex-1 min-w-0 overflow-hidden border-r border-dark-border">
                  <div ref="leftColContent" class="font-mono text-sm leading-6" :style="{ minHeight: totalHeight + 'px', width: 'max-content', transform: `translateX(-${sbsLeftScroll}px)` }">
                    <div v-for="(line, i) in lines" :key="'L'+i" class="min-h-6 min-w-full border-l-[3px] border-dark-border/30" :class="[cellClass('left', line.type), isMatchHighlighted(i) && 'bg-yellow-500/20']">
                      <div class="flex" style="width: max-content">
                        <span class="inline-block min-w-10 text-right tabular-nums text-slate-400 px-2 flex-shrink-0">{{ formatLineNum(line.oldLineNum) }}</span>
                        <span class="whitespace-pre px-2 text-gray-200">{{ showLeft(line.type) ? line.content : '\u00a0' }}</span>
                      </div>
                    </div>
                  </div>
                </div>
                <div class="flex-1 min-w-0 overflow-hidden">
                  <div ref="rightColContent" class="font-mono text-sm leading-6" :style="{ minHeight: totalHeight + 'px', width: 'max-content', transform: `translateX(-${sbsRightScroll}px)` }">
                    <div v-for="(line, i) in lines" :key="'R'+i" class="min-h-6 min-w-full border-l-[3px] border-dark-border/30" :class="[cellClass('right', line.type), isMatchHighlighted(i) && 'bg-yellow-500/20']">
                      <div class="flex" style="width: max-content">
                        <span class="inline-block min-w-10 text-right tabular-nums text-slate-400 px-2 flex-shrink-0">{{ formatLineNum(line.newLineNum) }}</span>
                        <span class="whitespace-pre px-2 text-gray-200">{{ (line.type === 'add' || line.type === 'context') ? line.content : '\u00a0' }}</span>
                      </div>
                    </div>
                  </div>
                </div>
              </div>
            </div>
            <div class="flex flex-shrink-0 border-t border-dark-border/60 overflow-hidden" style="height: 16px">
              <div ref="leftHScroll" class="flex-1 min-w-0 overflow-x-auto overflow-y-hidden border-r border-dark-border/50" style="height: 16px" @scroll="sbsLeftScroll = $event.target.scrollLeft">
                <div :style="{ width: leftScrollWidth + 'px', height: '1px' }" />
              </div>
              <div ref="rightHScroll" class="flex-1 min-w-0 overflow-x-auto overflow-y-hidden" style="height: 16px" @scroll="sbsRightScroll = $event.target.scrollLeft">
                <div :style="{ width: rightScrollWidth + 'px', height: '1px' }" />
              </div>
            </div>
          </div>
        </template>
        <!-- Unified -->
        <template v-else>
          <div ref="scrollContainer" class="flex-1 min-h-0 overflow-auto bg-dark-panel" @scroll="scrollTop = $event.target.scrollTop">
            <div v-if="loading" class="p-8 text-center text-gray-400">No differences found. (Or still loading...)</div>
            <div v-else-if="lines.length > 0" class="font-mono text-sm leading-6" style="min-width: 100%; width: max-content;">
              <div v-if="useVirtualScroll" :style="{ height: totalHeight + 'px', position: 'relative' }" class="w-full">
                <div :style="{ transform: `translateY(${visibleStart * 24}px)`, minWidth: '100%', width: 'max-content' }" class="font-mono text-sm leading-6 absolute left-0 right-0 top-0">
                  <div v-for="(line, i) in visibleLines" :key="visibleStart + i" :ref="el => setLineRef(visibleStart + i, el)"
                    class="min-h-6 min-w-full border-l-[3px]" :class="[lineClasses(line.type), isMatchHighlighted(visibleStart + i) && 'bg-yellow-500/20']">
                    <div class="flex min-w-full" style="width: max-content">
                      <div class="flex min-w-32 border-r border-dark-border/50 bg-dark-input/50 px-3 items-center justify-end gap-4 select-none flex-shrink-0">
                        <span class="inline-block min-w-10 text-right tabular-nums text-slate-400" :class="{ 'text-gray-500': !line.oldLineNum }">{{ formatLineNum(line.oldLineNum) }}</span>
                        <span class="inline-block min-w-10 text-right tabular-nums text-slate-400" :class="{ 'text-gray-500': !line.newLineNum }">{{ formatLineNum(line.newLineNum) }}</span>
                      </div>
                      <div class="flex px-3 items-center flex-nowrap">
                        <span v-if="line.type !== 'header' && line.type !== 'other'" class="inline-block min-w-4 mr-2 font-semibold select-none flex-shrink-0" :class="prefixColor(line.type)">{{ prefix(line.type) }}</span>
                        <span class="whitespace-pre">{{ line.content }}</span>
                      </div>
                    </div>
                  </div>
                </div>
              </div>
              <template v-else>
                <div v-for="(line, index) in lines" :key="index" :ref="el => setLineRef(index, el)"
                  class="min-h-6 min-w-full border-l-[3px]" :class="[lineClasses(line.type), isMatchHighlighted(index) && 'bg-yellow-500/20']">
                  <div class="flex min-w-full" style="width: max-content">
                    <div class="flex min-w-32 border-r border-dark-border/50 bg-dark-input/50 px-3 items-center justify-end gap-4 select-none flex-shrink-0">
                      <span class="inline-block min-w-10 text-right tabular-nums text-slate-400" :class="{ 'text-gray-500': !line.oldLineNum }">{{ formatLineNum(line.oldLineNum) }}</span>
                      <span class="inline-block min-w-10 text-right tabular-nums text-slate-400" :class="{ 'text-gray-500': !line.newLineNum }">{{ formatLineNum(line.newLineNum) }}</span>
                    </div>
                    <div class="flex px-3 items-center flex-nowrap">
                      <span v-if="line.type !== 'header' && line.type !== 'other'" class="inline-block min-w-4 mr-2 font-semibold select-none flex-shrink-0" :class="prefixColor(line.type)">{{ prefix(line.type) }}</span>
                      <span class="whitespace-pre">{{ line.content }}</span>
                    </div>
                  </div>
                </div>
              </template>
            </div>
          </div>
        </template>
      </div>
    </div>
  </div>
</template>

<script>
const ROW_HEIGHT = 24
const VIRTUAL_THRESHOLD = 800
const OVERSCAN = 20

export default {
  name: 'DiffViewer',
  props: { visible: { type: Boolean, default: false }, lines: { type: Array, default: () => [] }, loading: { type: Boolean, default: false } },
  data() {
    return {
      searchQuery: '',
      searchMatches: [],
      currentMatchIndex: -1,
      lineRefs: {},
      viewMode: 'unified',
      scrollTop: 0,
      containerHeight: 600,
      sbsLeftScroll: 0,
      sbsRightScroll: 0,
      leftScrollWidth: 0,
      rightScrollWidth: 0
    }
  },
  computed: {
    useVirtualScroll() { return this.lines.length > VIRTUAL_THRESHOLD },
    totalHeight() { return this.lines.length * ROW_HEIGHT },
    visibleStart() { return this.useVirtualScroll ? Math.max(0, Math.floor(this.scrollTop / ROW_HEIGHT) - OVERSCAN) : 0 },
    visibleEnd() { return this.useVirtualScroll ? Math.min(this.lines.length, this.visibleStart + Math.ceil(this.containerHeight / ROW_HEIGHT) + OVERSCAN * 2) : this.lines.length },
    visibleLines() { return this.lines.slice(this.visibleStart, this.visibleEnd) }
  },
  mounted() {
    window.addEventListener('keydown', this.onKeyDown)
    const wrap = () => this.$refs.contentWrap
    this._ro = new ResizeObserver(() => { const w = wrap(); if (w) this.containerHeight = w.clientHeight })
    this.$nextTick(() => { const w = wrap(); if (w) this._ro.observe(w) })
  },
  beforeUnmount() {
    document.body.style.overflow = ''
    window.removeEventListener('keydown', this.onKeyDown)
    this._ro?.disconnect()
  },
  updated() {
    if (this.viewMode === 'sidebyside' && this.lines.length > 0) this.$nextTick(this.measureSbsWidths)
  },
  watch: {
    visible(v) {
      document.body.style.overflow = v ? 'hidden' : ''
      if (v) { this.scrollTop = 0; this.$nextTick(() => { const e = this.$refs.scrollContainer || this.$refs.sideBySideScroll; if (e) e.scrollTop = 0; if (this.$refs.contentWrap) this.containerHeight = this.$refs.contentWrap.clientHeight }) }
    },
    lines() {
      this.scrollTop = 0
      const e = this.$refs.scrollContainer || this.$refs.sideBySideScroll
      if (e) e.scrollTop = 0
      if (this.searchQuery) this.performSearch()
      this.$nextTick(() => {
        if (this.$refs.contentWrap) this.containerHeight = this.$refs.contentWrap.clientHeight
        if (this.viewMode === 'sidebyside') {
          this.sbsLeftScroll = 0
          this.sbsRightScroll = 0
          this.$nextTick(() => {
            this.measureSbsWidths()
            if (this.$refs.leftHScroll) this.$refs.leftHScroll.scrollLeft = 0
            if (this.$refs.rightHScroll) this.$refs.rightHScroll.scrollLeft = 0
          })
        }
      })
    }
  },
  methods: {
    measureSbsWidths() {
      const l = this.$refs.leftColContent, r = this.$refs.rightColContent
      this.leftScrollWidth = l?.scrollWidth ?? 0
      this.rightScrollWidth = r?.scrollWidth ?? 0
    },
    showLeft(t) { return ['remove','context','header','other'].includes(t) },
    cellClass(side, type) {
      const base = side === 'left' ? 'bg-dark-input/30' : 'bg-dark-input/20'
      if (type === 'remove' && side === 'left') return base + ' bg-diff-remove'
      if (type === 'add' && side === 'right') return base + ' bg-diff-add'
      if (['header','other'].includes(type)) return base + ' bg-diff-header'
      return base
    },
    prefix(t) { return { add: '+', remove: '-', context: ' ' }[t] || ' ' },
    prefixColor(t) { return { add: 'text-emerald-500/90', remove: 'text-red-500/90' }[t] || '' },
    lineClasses(t) { return { add: 'bg-diff-add border-l-emerald-500/50', remove: 'bg-diff-remove border-l-red-500/50', header: 'bg-diff-header border-l-transparent', context: 'bg-diff-context border-l-transparent' }[t] || 'border-l-transparent' },
    formatLineNum(n) { return n ?? '' },
    onKeyDown(e) { if ((e.ctrlKey || e.metaKey) && e.key === 'f') { e.preventDefault(); this.$refs.searchInput?.focus(); this.$refs.searchInput?.select() } },
    performSearch() {
      const q = this.searchQuery.trim().toLowerCase()
      this.searchMatches = q ? this.lines.map((l, i) => l.content?.toLowerCase().includes(q) ? i : -1).filter(i => i !== -1) : []
      this.currentMatchIndex = this.searchMatches.length ? 0 : -1
      if (this.currentMatchIndex >= 0) this.scrollToMatch()
    },
    nextMatch() { if (!this.searchMatches.length) return; this.currentMatchIndex = (this.currentMatchIndex + 1) % this.searchMatches.length; this.scrollToMatch() },
    prevMatch() { if (!this.searchMatches.length) return; this.currentMatchIndex = this.currentMatchIndex <= 0 ? this.searchMatches.length - 1 : this.currentMatchIndex - 1; this.scrollToMatch() },
    scrollToMatch() {
      const idx = this.searchMatches[this.currentMatchIndex]
      if (idx == null) return
      const el = this.lineRefs[idx]
      if (el) el.scrollIntoView({ behavior: 'smooth', block: 'center' })
      else {
        const c = this.$refs.scrollContainer || this.$refs.sideBySideScroll
        if (c) c.scrollTop = idx * 24 - c.clientHeight / 2 + 12
      }
    },
    isMatchHighlighted(i) { return this.searchMatches[this.currentMatchIndex] === i },
    clearSearch() { this.searchQuery = ''; this.searchMatches = []; this.currentMatchIndex = -1 },
    setLineRef(i, el) { if (el != null) this.lineRefs[i] = el }
  }
}
</script>
