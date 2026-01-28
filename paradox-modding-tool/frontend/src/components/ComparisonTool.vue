<template>
  <div class="flex-1 flex flex-col min-h-0 min-w-0 p-4 sm:p-6 overflow-auto bg-dark-input">
    <div
      class="w-full max-w-full bg-dark-panel/60 backdrop-blur-sm rounded-xl p-4 sm:p-6 shadow-material border border-dark-border/50 mb-4 sm:mb-6 flex-shrink-0">
      <h2 class="text-lg sm:text-xl font-semibold mb-4 sm:mb-5">Comparison Tool</h2>

      <!-- FileSet A -->
      <div class="mb-4">
        <label class="block mb-2 font-medium text-sm sm:text-base">Compare FileSet/Directory A:</label>
        <textarea :value="setA.join('\n')" @input="setA = $event.target.value.split('\n').filter(p => p.trim())"
          rows="3" placeholder="Select files or directories..."
          class="w-full min-w-0 px-3 py-2 bg-dark-input/80 border border-dark-border rounded-lg text-gray-200 placeholder:text-gray-400 focus:outline-none focus:ring-2 focus:ring-btn-primary/50 focus:border-btn-primary font-mono text-sm transition-all duration-200">
        </textarea>
        <div class="flex flex-wrap gap-2 mt-2">
          <button @click="selectFiles('Select Multiple Files To Compare (A)', '*.txt', 'setA')" class="btn-primary">
            Select File(s)
          </button>
          <button @click="selectFolder('Select Folder To Compare (A)', 'setA')" class="btn-primary">
            Select Folder
          </button>
          <button @click="clearSet('setA')" class="btn-secondary">Clear</button>
        </div>
      </div>

      <!-- FileSet B -->
      <div class="mb-4">
        <label class="block mb-2 font-medium text-sm sm:text-base">Compare FileSet/Directory B:</label>
        <textarea :value="setB.join('\n')" @input="setB = $event.target.value.split('\n').filter(p => p.trim())"
          rows="3" placeholder="Select files or directories..."
          class="w-full min-w-0 px-3 py-2 bg-dark-input/80 border border-dark-border rounded-lg text-gray-200 placeholder:text-gray-400 focus:outline-none focus:ring-2 focus:ring-btn-primary/50 focus:border-btn-primary font-mono text-sm transition-all duration-200">
        </textarea>
        <div class="flex flex-wrap gap-2 mt-2">
          <button @click="selectFiles('Select Multiple Files To Compare (B)', '*.txt', 'setB')" class="btn-primary">
            Select File(s)
          </button>
          <button @click="selectFolder('Select Folder To Compare (B)', 'setB')" class="btn-primary">
            Select Folder
          </button>
          <button @click="clearSet('setB')" class="btn-secondary">Clear</button>
        </div>
      </div>

      <!-- Compare Button -->
      <button @click="updateMatchingFiles" :disabled="loadingFiles || setA.length === 0 || setB.length === 0"
        class="w-full mt-4 btn-primary disabled:opacity-50 disabled:cursor-not-allowed disabled:hover:shadow-material">
        {{ loadingFiles ? 'Finding matching files for comparison...' : 'Compare' }}
      </button>
    </div>

    <!-- File List (table-like, compact rows, sticky header) -->
    <div v-if="matchingFiles.length > 0"
      class="flex-1 min-w-0 w-full max-w-full bg-dark-panel/60 backdrop-blur-sm rounded-xl p-4 sm:p-6 shadow-material border border-dark-border/50 overflow-hidden flex flex-col"
      style="min-height: 200px;">
      <h3 class="text-base sm:text-lg font-semibold mb-3 flex-shrink-0">Matching Files ({{ matchingFiles.length }})</h3>
      <div class="flex-1 min-h-0 overflow-auto border border-dark-border/50 rounded-lg">
        <table class="w-full border-collapse text-sm font-mono">
          <thead class="sticky top-0 z-10 bg-dark-panel/95 border-b border-dark-border">
            <tr>
              <th class="text-left py-2 px-3 text-slate-400 font-medium">Relative path</th>
              <th class="text-left py-2 px-3 text-slate-400 font-medium w-24">Action</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(file, index) in matchingFiles" :key="index"
              :class="index % 2 === 0 ? 'bg-dark-input/40' : 'bg-dark-input/20'"
              class="border-b border-dark-border/30 hover:bg-dark-input transition-colors">
              <td class="py-1.5 px-3 text-gray-200 truncate max-w-0" :title="file.relativePath">{{ file.relativePath }}
              </td>
              <td class="py-1.5 px-3">
                <button type="button" @click="viewFileDiff(file)" class="btn-accent">
                  View diff
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
    <div v-else-if="!loadingFiles"
      class="w-full max-w-full bg-dark-panel/60 backdrop-blur-sm rounded-xl p-4 sm:p-6 shadow-material border border-dark-border/50 flex-shrink-0">
      <p class="text-gray-400 text-center text-sm sm:text-base">
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

export default {
  name: 'ComparisonTool',
  components: {
    DiffViewer
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
