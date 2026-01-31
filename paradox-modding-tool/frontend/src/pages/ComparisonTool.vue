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

<script>
import { GetDiff } from '../../bindings/paradox-modding-tool/diffservice.js'
import { CollectFilesFromPaths, FindMatchingFiles } from '../../bindings/paradox-modding-tool/fileservice.js'
import DiffViewer from '../components/DiffViewer.vue'
import FileSelector from '../components/FileSelector.vue'
import DataTable from 'primevue/datatable'
import Button from 'primevue/button'
import Column from 'primevue/column'

export default {
  name: 'ComparisonTool',
  components: {
    DiffViewer,
    FileSelector,
    Button,
    DataTable,
    Column
  },
  data() {
    return {
      setA: [],
      setB: [],
      matchingFiles: [],
      loadingFiles: false,
      diffLines: [],
      diffFilePath: null,
      loadingDiff: false
    }
  },
  methods: {
    onSetAUpdate(val) {
      this.setA = val
      this.matchingFiles = []
    },
    onSetBUpdate(val) {
      this.setB = val
      this.matchingFiles = []
    },
    async updateMatchingFiles() {
      if (this.setA.length === 0 || this.setB.length === 0) {
        alert('Please select at least one file or directory for both sets')
        this.matchingFiles = []
        return
      }

      this.loadingFiles = true
      try {
        // Collect files from both sets
        const filesA = await CollectFilesFromPaths(this.setA)
        const filesB = await CollectFilesFromPaths(this.setB)

        // Find matching files
        const matches = await FindMatchingFiles(filesA, filesB)

        // Convert to array format for display
        this.matchingFiles = Object.keys(matches).map(relativePath => ({
          relativePath,
          fileAPath: matches[relativePath].fileAPath,
          fileBPath: matches[relativePath].fileBPath
        }))
      } catch (error) {
        alert('Error finding matching files: ' + error)
        this.matchingFiles = []
      } finally {
        this.loadingFiles = false
      }
    },
    async viewFileDiff(file) {
      this.loadingDiff = true
      this.diffFilePath = file.relativePath
      try {
        this.diffLines = (await GetDiff(file.fileAPath, file.fileBPath)) || []
      } catch (e) {
        alert('Error loading diff: ' + e)
        this.diffLines = []
      } finally {
        this.loadingDiff = false
      }
    },
    closeDiff() {
      this.diffLines = []
      this.diffFilePath = null
      this.loadingDiff = false
    }
  }
}
</script>
