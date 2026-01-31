<template>
  <div class="flex-1 flex flex-col min-h-0 min-w-0 p-4 overflow-auto">
    <!-- Input Panel (InventoryTool-style) -->
    <div class="w-full max-w-full rounded-xl p-4 border border-dark-border mb-4">
      <h2 class="text-lg font-semibold mb-4">Comparison Tool</h2>

      <FileSelector :modelValue="setA" @update:modelValue="onSetAUpdate" label="FileSet / Directory A" class="mb-4"
        file-dialog-title="Select Multiple Files To Compare (A)"
        folder-dialog-title="Select Folder To Compare (A)" />
      <FileSelector :modelValue="setB" @update:modelValue="onSetBUpdate" label="FileSet / Directory B" class="mb-4"
        file-dialog-title="Select Multiple Files To Compare (B)"
        folder-dialog-title="Select Folder To Compare (B)" />

      <Button @click="updateMatchingFiles" :disabled="loadingFiles || setA.length === 0 || setB.length === 0"
        :label="loadingFiles ? 'Finding matching files...' : 'Compare'" />
    </div>

    <!-- Results (InventoryTool-style card) -->
    <div v-if="matchingFiles.length > 0" class="w-full max-w-full rounded-xl p-4 border border-dark-border mb-4 flex-1 min-h-0 flex flex-col">
      <h3 class="text-lg font-semibold mb-3">Matching Files</h3>
      <DataTable :value="matchingFiles" stripedRows class="flex-1">
        <Column field="relativePath" header="Relative path" />
        <Column header="Show Diff">
          <template #body="{ data }">
            <Button label="View diff" severity="success" variant="outlined" @click="viewFileDiff(data)" />
          </template>
        </Column>
      </DataTable>
    </div>

    <div v-else-if="!loadingFiles" class="w-full max-w-full rounded-xl p-4 border border-dark-border/50 bg-dark-panel/60">
      <p class="text-gray-300 text-center text-sm">
        Select both sets of files/directories and click "Compare"
      </p>
    </div>

    <!-- Diff Viewer Overlay -->
    <DiffViewer :visible="diffFilePath !== null" :lines="diffLines" :loading="loadingDiff" @close="closeDiff" />
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { GetDiff } from '../../bindings/paradox-modding-tool/diffservice.js'
import { CollectFilesFromPaths, FindMatchingFiles } from '../../bindings/paradox-modding-tool/fileservice.js'
import DiffViewer from '../components/DiffViewer.vue'
import FileSelector from '../components/FileSelector.vue'
import DataTable from 'primevue/datatable'
import Button from 'primevue/button'
import Column from 'primevue/column'

const setA = ref([])
const setB = ref([])
const matchingFiles = ref([])
const loadingFiles = ref(false)
const diffLines = ref([])
const diffFilePath = ref(null)
const loadingDiff = ref(false)

function onSetAUpdate(val) {
  setA.value = val
  matchingFiles.value = []
}

function onSetBUpdate(val) {
  setB.value = val
  matchingFiles.value = []
}

async function updateMatchingFiles() {
  if (setA.value.length === 0 || setB.value.length === 0) {
    alert('Please select at least one file or directory for both sets')
    matchingFiles.value = []
    return
  }

  loadingFiles.value = true
  try {
    const filesA = await CollectFilesFromPaths(setA.value)
    const filesB = await CollectFilesFromPaths(setB.value)
    const matches = await FindMatchingFiles(filesA, filesB)
    matchingFiles.value = Object.keys(matches).map(relativePath => ({
      relativePath,
      fileAPath: matches[relativePath].fileAPath,
      fileBPath: matches[relativePath].fileBPath
    }))
  } catch (error) {
    alert('Error finding matching files: ' + error)
    matchingFiles.value = []
  } finally {
    loadingFiles.value = false
  }
}

async function viewFileDiff(file) {
  loadingDiff.value = true
  diffFilePath.value = file.relativePath
  try {
    diffLines.value = (await GetDiff(file.fileAPath, file.fileBPath)) || []
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
