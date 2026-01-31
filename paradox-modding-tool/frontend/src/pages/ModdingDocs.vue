<template>
  <div class="p-4 flex-1 flex flex-col min-h-0">
    <Tabs v-model:value="activeTab" :pt="{ root: { class: 'flex-1 min-h-0 flex flex-col' } }">
      <TabList>
        <Tab value="0">Script docs</Tab>
        <Tab value="1">Modding wiki</Tab>
      </TabList>

      <!-- TODO: Need to fix scrolling, imrpove text editor and enable separate scroll (and position), maybe favoriting or some kind of better filter/collection of all the files? -->
      <TabPanels :pt="{ root: { class: 'flex-1 min-h-0 flex flex-col overflow-hidden' } }">
        <TabPanel value="0" class="flex flex-col gap-4">
          <Card>
            <template #content>
              <div class="flex flex-wrap items-end gap-4">
                <div class="flex flex-col gap-1">
                  <label class="text-sm font-medium">Game</label>
                  <Select v-model="game" :options="gameOptions" option-label="label" option-value="value" class="w-40"
                    @update:model-value="(v) => v && setGame(v)" />
                </div>
                <div class="flex flex-col gap-1 flex-1 min-w-[200px]">
                  <label class="text-sm font-medium">Game install path</label>
                  <InputText v-model="installPathDisplay" readonly placeholder="Set in Settings (gear icon in header)"
                    class="w-full" />
                </div>
                <Button label="Rescan" icon="pi pi-refresh" :loading="scanning" :disabled="!installPathDisplay"
                  @click="rescan" />
                <Button label="Check for updates" icon="pi pi-cloud" :loading="checkingUpdate"
                  :disabled="!installPathDisplay" severity="secondary" @click="checkForUpdates" />
              </div>
            </template>
          </Card>
          <Message v-if="scanError" severity="error" :closable="true" @close="scanError = ''">
            {{ scanError }}
          </Message>
          <Message v-else-if="updateDetected" severity="info" :closable="true" @close="updateDetected = false">
            Game update detected. Doc list was rescanned.
          </Message>
          <Card>
            <template #content>
              <div class="flex gap-4 flex-1 min-h-0 overflow-hidden">
                <DataTable :value="docEntries" data-key="relativePath" class="flex-1 overflow-auto" striped-rows
                  selection-mode="single" v-model:selection="selectedEntry" @row-select="onRowSelect">
                  <Column field="relativePath" header="Relative path" />
                </DataTable>
                <div
                  class="flex-1 min-w-0 flex flex-col border-l border-(--p-surface-200) dark:border-(--p-surface-700)">
                  <div class="p-2 border-b border-(--p-surface-200) dark:border-(--p-surface-700) font-medium text-sm">
                    {{ selectedEntry?.relativePath ?? 'Select a file' }}
                  </div>
                  <pre v-if="fileContent !== null"
                    class="flex-1 overflow-auto p-4 text-sm font-mono whitespace-pre-wrap border-0 m-0 bg-(--p-surface-50) dark:bg-(--p-surface-900) text-(--p-surface-800) dark:text-(--p-surface-200)">{{ fileContent }}</pre>
                  <p v-else-if="loadingContent" class="p-4 text-(--p-surface-500)">Loading…</p>
                  <p v-else class="p-4 text-(--p-surface-500)">Select a file to view content.</p>
                </div>
              </div>
            </template>
          </Card>
        </TabPanel>
        <TabPanel value="1" :pt="{ root: { class: 'flex-1 min-h-0 flex flex-col' } }">
          <EmbedPanel :url="wikiUrl" :title="'Modding wiki'" />
        </TabPanel>
      </TabPanels>
    </Tabs>
  </div>
</template>

<script setup>
import { ref, computed, watch } from 'vue'
import Tabs from 'primevue/tabs'
import TabList from 'primevue/tablist'
import Tab from 'primevue/tab'
import TabPanels from 'primevue/tabpanels'
import TabPanel from 'primevue/tabpanel'
import Select from 'primevue/select'
import InputText from 'primevue/inputtext'
import Button from 'primevue/button'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Message from 'primevue/message'
import Card from 'primevue/card'
import { useAppSettings } from '../composables/useAppSettings'
import {
  ListGameDocFiles,
  ReadFileContent,
  GetScriptRoot
} from '../../bindings/paradox-modding-tool/fileservice.js'
import {
  CheckGameUpdate,
  GetDocPathCache,
  SetDocPathCache,
  ClearDocPathCache
} from '../../bindings/paradox-modding-tool/settingsservice.js'
import EmbedPanel from '../components/EmbedPanel.vue'

const { game, gameInstallPath, setGame, setGameInstallPath, loadSettings } = useAppSettings()

const activeTab = ref('0')

const gameOptions = [
  { label: 'CK3', value: 'ck3' },
  { label: 'EU5', value: 'eu5' }
]

const installPathDisplay = computed(() => gameInstallPath.value)

const docEntries = ref([])
const selectedEntry = ref(null)
const fileContent = ref(null)
const loadingContent = ref(false)
const scanning = ref(false)
const scanError = ref('')
const checkingUpdate = ref(false)
const updateDetected = ref(false)

const wikiUrl = computed(() =>
  game.value === 'eu5'
    ? 'https://eu5.paradoxwikis.com/Modding'
    : 'https://ck3.paradoxwikis.com/Modding'
)


async function loadFromCacheOrScan() {
  const path = gameInstallPath.value?.trim()
  if (!path) {
    docEntries.value = []
    return
  }
  const cached = await GetDocPathCache(game.value, path).catch(() => null)
  if (cached?.paths?.length) {
    docEntries.value = cached.paths.map((relativePath) => ({ relativePath, fullPath: '' }))
    scanError.value = ''
    return
  }
  await rescan()
}

async function rescan() {
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
    let lastSeenUpdateId = undefined
    try {
      const updateResult = await CheckGameUpdate(game.value)
      if (updateResult?.latestUpdateId) lastSeenUpdateId = updateResult.latestUpdateId
    } catch {
      // ignore
    }
    await SetDocPathCache(game.value, path, {
      paths: docEntries.value.map((e) => e.relativePath),
      scannedAt: new Date().toISOString(),
      lastSeenUpdateId: lastSeenUpdateId ?? ''
    })
  } catch (e) {
    scanError.value = e?.message ?? String(e)
    docEntries.value = []
  } finally {
    scanning.value = false
  }
}

async function checkForUpdates() {
  const path = gameInstallPath.value?.trim()
  if (!path) return
  checkingUpdate.value = true
  updateDetected.value = false
  try {
    const result = await CheckGameUpdate(game.value)
    const cached = await GetDocPathCache(game.value, path).catch(() => null)
    const lastId = cached?.lastSeenUpdateId ?? ''
    const newId = result?.latestUpdateId ?? ''
    if (newId && newId !== lastId) {
      await ClearDocPathCache(game.value, path)
      updateDetected.value = true
      await rescan()
    }
  } catch {
    // ignore
  } finally {
    checkingUpdate.value = false
  }
}

async function onRowSelect() {
  const entry = selectedEntry.value
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
  loadFileContent(fullPath)
}

async function loadFileContent(fullPath) {
  if (!fullPath?.trim()) {
    fileContent.value = null
    return
  }
  loadingContent.value = true
  fileContent.value = null
  try {
    const content = await ReadFileContent(fullPath)
    fileContent.value = content ?? ''
  } catch {
    fileContent.value = ''
  } finally {
    loadingContent.value = false
  }
}

watch(
  [game, gameInstallPath],
  () => {
    loadFromCacheOrScan()
  },
  { immediate: true }
)
</script>
