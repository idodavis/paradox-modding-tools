<template>
  <div class="p-4">
    <Tabs v-model:value="activeMode">
      <TabList>
        <Tab value="vanilla">Vanilla vs mod</Tab>
        <Tab value="sets">Two sets / directories</Tab>
        <Tab value="any">Any two files</Tab>
      </TabList>
      <TabPanels>
        <!-- Mode 1: Vanilla vs mod -->
        <TabPanel value="vanilla" class="flex flex-col gap-4">
          <Card>
            <template #content>
              <p class="text-sm text-(--p-surface-600) dark:text-(--p-surface-400) mb-4">
                A = vanilla (game install path). B = your mod files or folder.
              </p>
              <div class="mb-4">
                <label class="block text-sm font-medium mb-2">Vanilla (A):</label>
                <InputText :model-value="gameInstallPath || ''" readonly class="w-full"
                  placeholder="Set game install path in Modding Docs or header settings" />
                <span class="text-xs text-(--p-surface-500)">Uses global game: {{ game.toUpperCase() }} (script root: {{
                  game === 'ck3' ? 'game' : 'game/in_game' }})</span>
              </div>
              <FileSelector v-model="modPaths" label="Mod (B)" file-dialog-title="Select Mod Files (B)"
                folder-dialog-title="Select Mod Folder (B)" class="mb-4" />
              <Button label="Compare" :disabled="loadingFiles || !gameInstallPath || modPaths.length === 0"
                :loading="loadingFiles" @click="runVanillaCompare" />
            </template>
          </Card>
          <Card v-if="matchingFiles.length > 0" class="flex-1 min-h-0 flex flex-col overflow-hidden">
            <template #content>
              <h3 class="text-lg font-semibold mb-3">Matching Files</h3>
              <DataTable :value="matchingFiles" striped-rows class="flex-1">
                <Column field="relativePath" header="Relative path" />
                <Column header="Show Diff">
                  <template #body="{ data }">
                    <Button label="View diff" severity="success" variant="outlined" @click="viewFileDiff(data)" />
                  </template>
                </Column>
              </DataTable>
            </template>
          </Card>
          <p v-else-if="!loadingFiles" class="text-(--p-surface-500) text-sm">Set game install path and select mod
            files/folder,
            then click Compare.</p>
        </TabPanel>

        <!-- Mode 2: Two sets / directories -->
        <TabPanel value="sets" class="flex-1 min-h-0 flex flex-col gap-4">
          <Card>
            <template #content>
              <FileSelector v-model="setA" label="File Set / Directory A"
                file-dialog-title="Select Multiple Files To Compare (A)"
                folder-dialog-title="Select Folder To Compare (A)" class="mb-4" />
              <FileSelector v-model="setB" label="File Set / Directory B"
                file-dialog-title="Select Multiple Files To Compare (B)"
                folder-dialog-title="Select Folder To Compare (B)" class="mb-4" />
              <Button :label="loadingFiles ? 'Finding matching files...' : 'Compare'"
                :disabled="loadingFiles || setA.length === 0 || setB.length === 0" @click="updateMatchingFiles" />
            </template>
          </Card>
          <Card v-if="matchingFiles.length > 0" class="flex-1 min-h-0 flex flex-col overflow-hidden">
            <template #content>
              <h3 class="text-lg font-semibold mb-3">Matching Files</h3>
              <DataTable :value="matchingFiles" striped-rows class="flex-1">
                <Column field="relativePath" header="Relative path" />
                <Column header="Show Diff">
                  <template #body="{ data }">
                    <Button label="View diff" severity="success" variant="outlined" @click="viewFileDiff(data)" />
                  </template>
                </Column>
              </DataTable>
            </template>
          </Card>
          <p v-else-if="!loadingFiles" class="text-(--p-surface-500) text-sm">Select both sets of files/directories and
            click
            Compare.</p>
        </TabPanel>

        <!-- Mode 3: Any two files -->
        <TabPanel value="any" class="flex-1 min-h-0 flex flex-col gap-4">
          <Card>
            <template #content>
              <div class="flex flex-wrap gap-4 items-end mb-4">
                <div class="flex flex-col gap-1 min-w-[200px]">
                  <label class="text-sm font-medium">File A</label>
                  <InputText :model-value="fileAPath" readonly placeholder="Select file A" class="w-full" />
                  <Button label="Select file A" icon="pi pi-file" size="small" class="mt-1" @click="selectFileA" />
                </div>
                <div class="flex flex-col gap-1 min-w-[200px]">
                  <label class="text-sm font-medium">File B</label>
                  <InputText :model-value="fileBPath" readonly placeholder="Select file B" class="w-full" />
                  <Button label="Select file B" icon="pi pi-file" size="small" class="mt-1" @click="selectFileB" />
                </div>
              </div>
              <Button label="Compare" :disabled="loadingFiles || !fileAPath || !fileBPath" :loading="loadingFiles"
                @click="compareTwoFiles" />
            </template>
          </Card>
          <Card v-if="anyTwoResult" class="flex-1 min-h-0 flex flex-col overflow-hidden">
            <template #content>
              <h3 class="text-lg font-semibold mb-3">Diff</h3>
              <p class="text-sm text-(--p-surface-500) mb-2">{{ fileAPath }} vs {{ fileBPath }}</p>
              <Button label="View diff" severity="success" variant="outlined" @click="viewFileDiff(anyTwoResult)" />
            </template>
          </Card>
          <p v-else-if="!loadingFiles && !fileAPath && !fileBPath" class="text-(--p-surface-500) text-sm">
            Select file A and file B, then click Compare.
          </p>
        </TabPanel>
      </TabPanels>
    </Tabs>

    <DiffViewer :visible="diffFilePath !== null" :lines="diffLines" :loading="loadingDiff" @close="closeDiff" />
  </div>
</template>

<script setup>
import { ref, computed } from 'vue'
import Tabs from 'primevue/tabs'
import TabList from 'primevue/tablist'
import Tab from 'primevue/tab'
import TabPanels from 'primevue/tabpanels'
import TabPanel from 'primevue/tabpanel'
import InputText from 'primevue/inputtext'
import Button from 'primevue/button'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Card from 'primevue/card'
import { GetDiff } from '../../bindings/paradox-modding-tool/diffservice.js'
import {
  CollectFilesFromPaths,
  FindMatchingFiles,
  GetScriptRoot,
  SelectSingleFile
} from '../../bindings/paradox-modding-tool/fileservice.js'
import DiffViewer from '../components/DiffViewer.vue'
import FileSelector from '../components/FileSelector.vue'
import { useAppSettings } from '../composables/useAppSettings'

const { game, gameInstallPath } = useAppSettings()

const activeMode = ref('sets')
const setA = ref([])
const setB = ref([])
const modPaths = ref([])
const fileAPath = ref('')
const fileBPath = ref('')
const matchingFiles = ref([])
const loadingFiles = ref(false)
const diffLines = ref([])
const diffFilePath = ref(null)
const loadingDiff = ref(false)
const anyTwoResult = ref(null)

function onSetAUpdate(val) {
  setA.value = val
  matchingFiles.value = []
}

function onSetBUpdate(val) {
  setB.value = val
  matchingFiles.value = []
}

async function runVanillaCompare() {
  const path = gameInstallPath.value?.trim()
  if (!path || modPaths.value.length === 0) {
    matchingFiles.value = []
    return
  }
  loadingFiles.value = true
  matchingFiles.value = []
  try {
    const root = await GetScriptRoot(path, game.value)
    const filesA = await CollectFilesFromPaths([root])
    const filesB = await CollectFilesFromPaths(modPaths.value)
    const matches = await FindMatchingFiles(filesA, filesB)
    matchingFiles.value = Object.keys(matches).map((relativePath) => ({
      relativePath,
      fileAPath: matches[relativePath].fileAPath,
      fileBPath: matches[relativePath].fileBPath
    }))
  } catch (e) {
    alert('Error: ' + (e?.message ?? e))
    matchingFiles.value = []
  } finally {
    loadingFiles.value = false
  }
}

async function updateMatchingFiles() {
  if (setA.value.length === 0 || setB.value.length === 0) {
    matchingFiles.value = []
    return
  }
  loadingFiles.value = true
  try {
    const filesA = await CollectFilesFromPaths(setA.value)
    const filesB = await CollectFilesFromPaths(setB.value)
    const matches = await FindMatchingFiles(filesA, filesB)
    matchingFiles.value = Object.keys(matches).map((relativePath) => ({
      relativePath,
      fileAPath: matches[relativePath].fileAPath,
      fileBPath: matches[relativePath].fileBPath
    }))
  } catch (e) {
    alert('Error finding matching files: ' + e)
    matchingFiles.value = []
  } finally {
    loadingFiles.value = false
  }
}

async function selectFileA() {
  try {
    const path = await SelectSingleFile('Select file A', '*.txt')
    if (path) fileAPath.value = path
  } catch (e) {
    alert('Error: ' + (e?.message ?? e))
  }
}

async function selectFileB() {
  try {
    const path = await SelectSingleFile('Select file B', '*.txt')
    if (path) fileBPath.value = path
  } catch (e) {
    alert('Error: ' + (e?.message ?? e))
  }
}

async function compareTwoFiles() {
  if (!fileAPath.value || !fileBPath.value) return
  loadingFiles.value = true
  anyTwoResult.value = null
  try {
    const lines = await GetDiff(fileAPath.value, fileBPath.value)
    anyTwoResult.value = {
      relativePath: 'A vs B',
      fileAPath: fileAPath.value,
      fileBPath: fileBPath.value,
      _lines: lines ?? []
    }
  } catch (e) {
    alert('Error loading diff: ' + e)
  } finally {
    loadingFiles.value = false
  }
}

async function viewFileDiff(file) {
  loadingDiff.value = true
  diffFilePath.value = file?.relativePath ?? 'Diff'
  try {
    if (file?._lines) {
      diffLines.value = file._lines
    } else {
      diffLines.value = (await GetDiff(file.fileAPath, file.fileBPath)) ?? []
    }
  } catch (e) {
    alert('Error loading diff: ' + e)
    diffLines.value = []
  } finally {
    loadingDiff.value = false
  }
}

function closeDiff() {
  diffLines.value = []
  diffFilePath.value = null
  loadingDiff.value = false
}
</script>
