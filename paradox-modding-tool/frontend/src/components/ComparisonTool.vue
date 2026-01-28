<template>
  <div class="flex-1 flex flex-col min-h-0 min-w-0 p-4 sm:p-6 overflow-auto">
    <div class="w-full max-w-full rounded-xl p-4 sm:p-6 border mb-4 sm:mb-6 shrink-0">
      <h2 class="text-lg sm:text-xl font-semibold mb-4 sm:mb-5">Comparison Tool</h2>

      <!-- FileSet A -->
      <div class="mb-4">
        <label class="block mb-2 font-medium text-sm sm:text-base">Compare FileSet/Directory A:</label>
        <Textarea :modelValue="setA.join('\n')" @update:modelValue="setA = normalizeLines($event)" rows="3"
          placeholder="Select files or directories..." class="w-full min-w-0 px-3 py-2" />
        <div class="flex flex-wrap gap-2 mt-2">
          <Button label="Select File(s)"
            @click="selectFiles('Select Multiple Files To Compare (A)', '*.txt', 'setA')" />
          <Button label="Select Folder" @click="selectFolder('Select Folder To Compare (A)', 'setA')" />
          <Button label="Clear" @click="clearSet('setA')" />
        </div>
      </div>

      <!-- FileSet B -->
      <div class="mb-4">
        <label class="block mb-2 font-medium text-sm sm:text-base">Compare FileSet/Directory B:</label>
        <Textarea :modelValue="setB.join('\n')" @update:modelValue="setB = normalizeLines($event)" rows="3"
          placeholder="Select files or directories..." class="w-full min-w-0 px-3 py-2" />
        <div class="flex flex-wrap gap-2 mt-2">
          <Button label="Select File(s)"
            @click="selectFiles('Select Multiple Files To Compare (B)', '*.txt', 'setB')" />
          <Button label="Select Folder" @click="selectFolder('Select Folder To Compare (B)', 'setB')" />
          <Button label="Clear" @click="clearSet('setB')" />
        </div>
      </div>

      <!-- Compare Button -->
      <Button @click="updateMatchingFiles" :disabled="loadingFiles || setA.length === 0 || setB.length === 0"
        class="w-full mt-4" :label="loadingFiles ? 'Finding matching files for comparison...' : 'Compare'" />
    </div>

    <!-- File List (PrimeVue DataTable) -->
    <DataTable v-if="matchingFiles.length > 0" :value="matchingFiles" stripedRows>
      <template #header>
        <span class="text-xl font-bold">Matching Files</span>
      </template>
      <Column field="relativePath" header="Relative path">
      </Column>
      <Column header="Show Diff">
        <template #body="{ data }">
          <Button label="View diff" @click="viewFileDiff(data)" />
        </template>
      </Column>
    </DataTable>

    <div v-else-if="!loadingFiles"
      class="w-full max-w-full rounded-xl p-4 sm:p-6 border border-dark-border/50 bg-dark-panel/60 shrink-0">
      <p class="text-gray-300 text-center text-sm sm:text-base">
        Please select both sets of files/directories and click "Compare"
      </p>
    </div>

    <!-- Diff Viewer Overlay -->
    <DiffViewer :visible="diffFilePath !== null" :lines="diffLines" :loading="loadingDiff" @close="closeDiff" />
  </div>
</template>

<script>
import { GetDiff } from '../../bindings/paradox-modding-tool/diffservice.js'
import { SelectDirectory, SelectMultipleFiles, CollectFilesFromPaths, FindMatchingFiles } from '../../bindings/paradox-modding-tool/fileservice.js'
import DiffViewer from './DiffViewer.vue'
import DataTable from 'primevue/datatable'
import Button from 'primevue/button'
import Column from 'primevue/column'
import Textarea from 'primevue/textarea'

export default {
  name: 'ComparisonTool',
  components: {
    DiffViewer,
    Button,
    DataTable,
    Column,
    Textarea
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
    normalizeLines(value) {
      return (value || '').split(/\r?\n/).map(p => p.trim()).filter(Boolean)
    },
    async selectFiles(dialogTitle, filter, fileSet) {
      try {
        const selected = await SelectMultipleFiles(dialogTitle, filter)
        if (selected && selected.length > 0) {
          // Add new files, avoiding duplicates
          const existing = new Set(this[fileSet])
          for (const file of selected) {
            if (!existing.has(file)) {
              this[fileSet].push(file)
            }
          }
        }
      } catch (error) {
        alert('Error selecting files: ' + error)
      }
    },
    async selectFolder(dialogTitle, fileSet) {
      try {
        const selected = await SelectDirectory(dialogTitle)
        if (selected) {
          this[fileSet].push(selected)
        }
      } catch (error) {
        alert('Error selecting folder: ' + error)
      }
    },
    clearSet(fileSet) {
      this[fileSet] = []
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
