<template>
  <div class="p-2 flex-1 flex flex-col min-h-0 bg-dark-input">
    <div
      class="p-2 text-lg font-medium border-b border-(--p-surface-700) text-(--p-primary-400) flex items-center justify-between gap-2">
      <span>{{ filename }}</span>
      <div v-if="showCopyButton || showFullScreenButton" class="flex items-center gap-1">
        <Button v-if="showCopyButton && !error" icon="pi pi-copy" rounded text size="medium" severity="secondary"
          aria-label="Copy code" @click="copyToClipboard(content)" />
        <Button v-if="showFullScreenButton && !error" icon="pi pi-window-maximize" rounded text size="medium"
          severity="secondary" aria-label="Full screen" @click="fullScreenVisible = true" />
      </div>
    </div>
    <div class="flex-1 min-h-0 flex flex-col overflow-hidden">
      <template v-if="error">
        <div class="p-4 text-sm text-red-400">{{ error }}</div>
      </template>
      <template v-else-if="html">
        <div class="flex-1 min-h-0 overflow-auto flex min-w-0">
          <pre class="code-viewer-gutter select-none text-right text-sm text-slate-500">{{ gutterText }}</pre>
          <div class="script-code-shiki" v-html="html" />
        </div>
      </template>
      <template v-else>
        <div class="p-4 text-sm text-slate-400">Loading Content…</div>
      </template>
    </div>

    <Drawer v-model:visible="fullScreenVisible" position="full" :header="filename" class="bg-dark-input!">
      <div class="flex-1 min-h-0 overflow-auto flex min-w-0">
        <pre class="code-viewer-gutter select-none text-right text-sm text-slate-500">{{ gutterText }}</pre>
        <div class="script-code-shiki" v-html="html" />
      </div>
    </Drawer>
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted } from 'vue'
import { createHighlighter } from 'shiki'
import { copyToClipboard } from '../utils/general'

const props = defineProps({
  content: { type: String, default: '' },
  filename: { type: String, default: '' },
  showCopyButton: { type: Boolean, default: true },
  showFullScreenButton: { type: Boolean, default: true }
})

const html = ref('')
const fullScreenVisible = ref(false)
const error = ref('')
let highlighter = null

const gutterText = computed(() => {
  const text = props.content ?? ''
  if (!text) return '1'
  const n = text.split('\n').length
  return Array.from({ length: n }, (_, i) => i + 1).join('\n')
})

onMounted(async () => {
  try {
    highlighter = await createHighlighter({
      themes: ['one-dark-pro'],
      langs: ['hcl']
    })
    if (props.content) await highlight(props.content)
  } catch (e) {
    error.value = e?.message ?? 'Failed to load highlighter'
  }
})

async function highlight(code) {
  if (!highlighter || code == null) return
  try {
    error.value = ''
    html.value = await highlighter.codeToHtml(code, {
      lang: 'hcl',
      theme: 'one-dark-pro'
    })
  } catch (e) {
    error.value = e?.message ?? 'Highlight failed'
    html.value = ''
  }
}

watch(
  () => props.content,
  (code) => {
    if (highlighter != null) highlight(code ?? '')
    else html.value = ''
  },
  { immediate: true }
)
</script>

<style scoped>
.script-code-shiki :deep(pre) {
  margin: 0;
  padding: 1rem 1rem 1rem 0.75rem;
  font-size: 0.875rem;
  line-height: 1.75;
  overflow: visible;
  background: transparent !important;
}

.script-code-shiki :deep(code) {
  font-family: ui-monospace, monospace;
}

.code-viewer-gutter {
  padding: 1rem 0.5rem 1rem 1rem;
  /* same top/bottom as code (1rem), right gap, left margin */
  line-height: 1.75;
  font-family: ui-monospace, monospace;
}
</style>
