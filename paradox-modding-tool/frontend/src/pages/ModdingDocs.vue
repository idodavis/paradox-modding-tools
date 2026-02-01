<template>
  <div class="p-2 flex-1 flex flex-col min-h-0">
    <Tabs v-model:value="activeTab">
      <TabList>
        <Tab value="0">Script docs</Tab>
        <Tab value="1">Modding wiki</Tab>
      </TabList>

      <TabPanels>
        <TabPanel value="0">
          <Card :pt="toolbarCardPt">
            <template #content>
              <div class="flex flex-wrap items-end gap-4">
                <div class="min-w-200">
                  <label>Game install path</label>
                  <InputText v-model="installPathDisplay" readonly placeholder="Set in Settings (gear icon in header)"
                    class="w-full" />
                </div>
                <Button label="Scan" icon="pi pi-refresh" :loading="scanning" :disabled="!installPathDisplay"
                  @click="scan" />
              </div>
            </template>
          </Card>
          <Message v-if="scanError" severity="error" :closable="true" @close="scanError = ''">
            {{ scanError }}
          </Message>
          <Card :pt="editorCardPt">
            <template #content>
              <div class="flex flex-1 min-h-0 overflow-hidden rounded-lg border border-(--p-surface-800)">
                <div class="flex-1 min-w-0 min-h-0 flex flex-col overflow-hidden rounded-l pr-1">
                  <InputText v-model="filterText" placeholder="Filter by filename"
                    class="w-full mb-1 py-1 px-2 text-[13px]" aria-label="Filter by filename" />
                  <div class="flex-1 min-h-0 overflow-auto">
                    <TreeTable v-model:selectionKeys="selectionKeys" v-model:expandedKeys="expandedKeys"
                      :value="docTreeNodes" selectionMode="single" :metaKeySelection="false" dataKey="key" scrollable
                      scrollHeight="flex" @node-select="onNodeSelect" class="w-full">
                      <template #empty>No files match.</template>
                      <Column field="name" header="File" expander />
                    </TreeTable>
                  </div>
                </div>
                <div class="flex-1 min-w-0 min-h-0 flex flex-col overflow-hidden rounded-r -ml-px">
                  <CodeViewer v-if="fileContent !== null" :content="fileContent"
                    :filename="selectedEntry?.relativePath ?? 'Select a file'" />
                  <p v-else class="p-4 text-slate-400 bg-dark-input">Select a file to view content.</p>
                </div>
              </div>
            </template>
          </Card>
        </TabPanel>
        <TabPanel value="1">
          <iframe v-if="wikiUrl" :src="wikiUrl" :title="'Modding Wiki'" class="flex-1 min-h-0" />
        </TabPanel>
      </TabPanels>
    </Tabs>
  </div>
</template>

<script setup>
import { ref, computed, watch, nextTick } from 'vue'
import { useAppSettings } from '../composables/useAppSettings'
import { buildDocTree } from '../utils/docTree.js'
import {
  ListGameDocFiles,
  ReadFileContent,
  GetScriptRoot
} from '../../bindings/paradox-modding-tool/fileservice.js'
import {
  GetDocPathCache,
  SetDocPathCache,
  ClearDocPathCache
} from '../../bindings/paradox-modding-tool/settingsservice.js'
import CodeViewer from '../components/CodeViewer.vue'

const { game, gameInstallPath, setGame, setGameInstallPath, loadSettings } = useAppSettings()

const activeTab = ref('0')

const installPathDisplay = computed(() => gameInstallPath.value)

const docEntries = ref([])
const filterText = ref('')
const selectionKeys = ref(null)
const expandedKeys = ref({})
const selectedEntry = ref(null)
const fileContent = ref(null)
const scanning = ref(false)
const scanError = ref('')

const filteredEntries = computed(() => {
  const text = (filterText.value || '').trim().toLowerCase()
  if (!text) return docEntries.value
  return docEntries.value.filter((e) =>
    (e.relativePath || '').toLowerCase().includes(text)
  )
})

const docTreeNodes = computed(() => buildDocTree(filteredEntries.value))

const wikiUrl = computed(() =>
  game.value === 'eu5'
    ? 'https://eu5.paradoxwikis.com/Modding'
    : 'https://ck3.paradoxwikis.com/Modding'
)

const toolbarCardPt = {
  content: { class: 'py-2 px-3' }
}

const editorCardPt = {
  root: { class: 'flex-1 min-h-0 flex flex-col overflow-hidden' },
  body: { class: 'flex-1 min-h-0 overflow-hidden flex flex-col' },
  content: { class: 'flex-1 min-h-0 overflow-hidden flex flex-col' }
}

/** Load file list from cache for current game+path only. No scan. If no cache, show empty. */
async function loadFromCache() {
  const path = gameInstallPath.value?.trim()
  filterText.value = ''
  expandedKeys.value = {}
  selectionKeys.value = {}
  selectedEntry.value = null
  fileContent.value = null
  if (!path) {
    docEntries.value = []
    scanError.value = ''
    return
  }
  try {
    const cached = await GetDocPathCache(game.value, path).catch(() => null)
    if (cached?.paths?.length) {
      docEntries.value = cached.paths.map((relativePath) => ({ relativePath, fullPath: '' }))
      scanError.value = ''
    } else {
      docEntries.value = []
      scanError.value = ''
    }
  } catch {
    docEntries.value = []
    scanError.value = ''
  }
}

async function scan() {
  const path = gameInstallPath.value?.trim()
  if (!path) {
    scanError.value = 'Select game install path first.'
    return
  }
  scanning.value = true
  scanError.value = ''
  try {
    const list = await ListGameDocFiles(game.value, path)
    docEntries.value = list ?? []
    await SetDocPathCache(game.value, path, {
      paths: docEntries.value.map((e) => e.relativePath),
      scannedAt: new Date().toISOString(),
    })
  } catch (e) {
    scanError.value = e?.message ?? String(e)
    docEntries.value = []
  } finally {
    scanning.value = false
  }
}

function onNodeSelect(node) {
  // Folder: toggle expand/collapse, don't select or load file
  if (node.children?.length) {
    const key = node.key
    const next = { ...expandedKeys.value }
    next[key] = !next[key]
    expandedKeys.value = next
    nextTick(() => {
      selectionKeys.value = {}
    })
    return
  }

  if (!node?.data?.relativePath) {
    selectionKeys.value = {}
    selectedEntry.value = null
    fileContent.value = null
    return
  }
  selectedEntry.value = { relativePath: node.data.relativePath, fullPath: '' }
  loadFileForEntry(selectedEntry.value)
}

async function loadFileForEntry(entry) {
  if (!entry?.relativePath) {
    fileContent.value = null
    return
  }
  let fullPath = entry.fullPath
  if (!fullPath && gameInstallPath.value?.trim()) {
    try {
      const root = await GetScriptRoot(gameInstallPath.value, game.value)
      fullPath = root.replace(/\\/g, '/') + '/' + entry.relativePath
    } catch {
      fullPath = ''
    }
  }
  if (!fullPath?.trim()) {
    fileContent.value = null
    return
  }
  fileContent.value = null
  try {
    const content = await ReadFileContent(fullPath)
    fileContent.value = content ?? ''
  } catch {
    fileContent.value = ''
  }
}

watch([game, gameInstallPath], () => {
  loadFromCache()
}, { immediate: true })
</script>
