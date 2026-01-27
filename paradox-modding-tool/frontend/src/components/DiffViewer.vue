<template>
  <div v-if="visible" class="fixed inset-0 bg-dark-bg/95 backdrop-blur-sm z-[60] flex flex-col"
    @click.self="$emit('close')" style="pointer-events: auto;">
    <div
      class="flex-1 flex flex-col min-h-0 min-w-0 m-2 sm:m-4 bg-dark-panel/95 backdrop-blur-md rounded-xl shadow-material-lg border border-dark-border overflow-hidden"
      @click.stop style="pointer-events: auto;">
      <div class="flex flex-col px-4 sm:px-6 py-4 border-b border-dark-border bg-dark-panel/50 flex-shrink-0">
        <div class="flex justify-between items-center gap-2 mb-3">
          <h2 class="text-lg sm:text-xl font-semibold truncate">Diff Viewer</h2>
          <button @click="$emit('close')" class="btn-primary flex-shrink-0">Close</button>
        </div>
        <!-- Search Bar -->
        <div class="flex flex-wrap items-center gap-2">
          <input ref="searchInput" v-model="searchQuery" @input="performSearch" @keydown.enter.prevent="nextMatch"
            @keydown.shift.enter.prevent="prevMatch" type="text" placeholder="Search (Ctrl+F)"
            class="flex-1 min-w-0 px-3 py-2 bg-dark-input/80 border border-dark-border rounded-lg text-gray-200 placeholder:text-gray-400 focus:outline-none focus:ring-2 focus:ring-btn-primary/50 focus:border-btn-primary transition-all duration-200" />
          <span v-if="searchQuery && searchMatches.length > 0" class="text-sm text-gray-400 whitespace-nowrap">
            {{ currentMatchIndex + 1 }} / {{ searchMatches.length }}
          </span>
          <button v-if="searchQuery" @click="clearSearch" class="btn-secondary">Clear</button>
          <button v-if="searchQuery && searchMatches.length > 0" @click="prevMatch" class="btn-secondary">↑ Prev</button>
          <button v-if="searchQuery && searchMatches.length > 0" @click="nextMatch" class="btn-secondary">Next ↓</button>
        </div>
      </div>
      <div class="flex-1 min-h-0 overflow-auto overflow-x-auto">
        <div v-if="loading" class="p-8 text-center text-gray-400">
          No differences found. (Or still loading...)
        </div>
        <div v-else-if="lines.length > 0" class="font-mono text-sm leading-6"
          style="will-change: scroll-position; display: block;">
          <div v-for="(line, index) in lines" :key="index" :ref="el => { if (el) lineRefs[index] = el }"
            class="flex min-h-6 border-l-[3px]"
            :class="[getLineClasses(line.type), isMatchHighlighted(index) && 'bg-yellow-500/20']"
            style="width: max-content; min-width: 100%;">
            <!-- Line Numbers -->
            <div
              class="flex min-w-32 border-r border-dark-border/50 bg-dark-input/50 px-3 items-center justify-end gap-4 select-none flex-shrink-0">
              <span class="inline-block min-w-10 text-right tabular-nums text-slate-400"
                :class="{ 'text-gray-500': !line.oldLineNum }">
                {{ formatLineNum(line.oldLineNum) }}
              </span>
              <span class="inline-block min-w-10 text-right tabular-nums text-slate-400"
                :class="{ 'text-gray-500': !line.newLineNum }">
                {{ formatLineNum(line.newLineNum) }}
              </span>
            </div>
            <!-- Line Content -->
            <div class="flex px-3 items-center flex-nowrap">
              <span v-if="line.type !== 'header' && line.type !== 'other'"
                class="inline-block min-w-4 mr-2 font-semibold select-none flex-shrink-0"
                :class="getPrefixColor(line.type)">
                {{ getPrefix(line.type) }}
              </span>
              <span class="whitespace-pre">{{ line.content }}</span>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script>
export default {
  name: 'DiffViewer',
  props: {
    visible: {
      type: Boolean,
      default: false
    },
    lines: {
      type: Array,
      default: () => []
    },
    loading: {
      type: Boolean,
      default: false
    }
  },
  data() {
    return {
      searchQuery: '',
      searchMatches: [],
      currentMatchIndex: -1,
      lineRefs: {}
    }
  },
  mounted() {
    // Add Ctrl+F keyboard shortcut
    window.addEventListener('keydown', this.handleKeyDown)
  },
  beforeUnmount() {
    // Ensure body scroll is restored when component is destroyed
    document.body.style.overflow = ''
    window.removeEventListener('keydown', this.handleKeyDown)
  },
  watch: {
    visible(newVal) {
      if (newVal) {
        // Prevent body scroll when overlay is open
        document.body.style.overflow = 'hidden'
      } else {
        // Restore body scroll when overlay is closed
        document.body.style.overflow = ''
      }
    },
    lines() {
      // Re-search when lines change
      if (this.searchQuery) {
        this.performSearch()
      }
    }
  },
  methods: {
    handleKeyDown(event) {
      // Ctrl+F or Cmd+F to focus search
      if ((event.ctrlKey || event.metaKey) && event.key === 'f') {
        event.preventDefault()
        if (this.$refs.searchInput) {
          this.$refs.searchInput.focus()
          this.$refs.searchInput.select()
        }
      }
    },
    performSearch() {
      const query = this.searchQuery.trim().toLowerCase()
      this.searchMatches = query ? this.lines
        .map((line, i) => line.content?.toLowerCase().includes(query) ? i : -1)
        .filter(i => i !== -1) : []
      this.currentMatchIndex = this.searchMatches.length > 0 ? 0 : -1
      if (this.currentMatchIndex >= 0) this.scrollToMatch()
    },
    nextMatch() {
      if (this.searchMatches.length === 0) return
      this.currentMatchIndex = (this.currentMatchIndex + 1) % this.searchMatches.length
      this.scrollToMatch()
    },
    prevMatch() {
      if (this.searchMatches.length === 0) return
      this.currentMatchIndex = this.currentMatchIndex <= 0 ? this.searchMatches.length - 1 : this.currentMatchIndex - 1
      this.scrollToMatch()
    },
    scrollToMatch() {
      const lineEl = this.lineRefs[this.searchMatches[this.currentMatchIndex]]
      if (lineEl) lineEl.scrollIntoView({ behavior: 'smooth', block: 'center' })
    },
    isMatchHighlighted(index) {
      return this.searchMatches[this.currentMatchIndex] === index
    },
    clearSearch() {
      this.searchQuery = ''
      this.searchMatches = []
      this.currentMatchIndex = -1
    },
    formatLineNum(num) {
      return num ?? ''
    },
    getPrefix(type) {
      return { add: '+', remove: '-', context: ' ' }[type] || ' '
    },
    getPrefixColor(type) {
      return { add: 'text-emerald-500/90', remove: 'text-red-500/90' }[type] || ''
    },
    getLineClasses(type) {
      return {
        add: 'bg-diff-add border-l-emerald-500/50',
        remove: 'bg-diff-remove border-l-red-500/50',
        header: 'bg-diff-header border-l-transparent',
        context: 'bg-diff-context border-l-transparent'
      }[type] || 'border-l-transparent'
    }
  }
}
</script>
