<template>
  <div class="flex-1 grid grid-cols-1 lg:grid-cols-2 gap-6 p-4 sm:p-6 overflow-auto bg-dark-input">
    <!-- Configuration Panel -->
    <div
      class="bg-dark-panel/60 backdrop-blur-sm rounded-xl p-4 sm:p-6 overflow-y-auto shadow-material border border-dark-border/50">
      <div class="flex justify-end mb-4">
        <button type="button" @click="clearState" class="btn-secondary text-sm py-1.5 px-3">
          Clear state
        </button>
      </div>
      <!-- Merge Options -->
      <div class="mb-6">
        <div class="mb-4">
          <h3 class="text-lg font-semibold mb-3">Merge Options</h3>

          <details class="mb-5 group rounded-lg border border-dark-border/50 bg-dark-input/60 overflow-hidden">
            <summary
              class="px-3 py-2 cursor-pointer text-sm text-gray-300 hover:bg-dark-border/20 flex items-center justify-between gap-2 [&::-webkit-details-marker]:hidden [&::marker]:hidden">
              <span class="font-medium text-gray-200">How precedence works</span>
              <span class="text-gray-500 group-open:rotate-180 transition-transform shrink-0 text-xs">▾</span>
            </summary>
            <div
              class="px-3 pb-3 pt-0 text-sm text-gray-300 leading-relaxed space-y-2 border-t border-dark-border/30 mt-0">
              <p><strong>A (base)</strong> wins for every key unless it’s in the <strong>key list</strong>; those use
                <strong>B</strong>.</p>
              <p class="text-gray-400">After a game update: put <em>vanilla</em> in A, <em>mod</em> in B, and list your
                mod’s object keys (events, decisions, etc.). Output = vanilla except where you changed things.</p>
            </div>
          </details>

          <div class="flex items-center cursor-pointer ml-4">
            <input type="checkbox" v-model="addAdditionalEntries"
              class="w-4 h-4 text-btn-primary bg-dark-input border-dark-border rounded focus:ring-btn-primary focus:ring-2" />
            <span class="ml-2">Add entries from B that don’t exist in A (e.g. mod-only events)</span>
          </div>

          <div v-if="addAdditionalEntries" class="mb-4 ml-8">
            <div class="my-2 text-sm text-gray-400">Additional Entry Placement:</div>
            <div class="space-y-2">
              <label class="flex items-center cursor-pointer">
                <input type="radio" v-model="entryPlacement" value="bottom"
                  class="w-4 h-4 text-btn-primary bg-dark-input border-dark-border focus:ring-btn-primary focus:ring-2" />
                <span class="ml-2">Bottom of file (with sectional comment)</span>
              </label>
              <label class="flex items-center cursor-pointer">
                <input type="radio" v-model="entryPlacement" value="preserve_order"
                  class="w-4 h-4 text-btn-primary bg-dark-input border-dark-border focus:ring-btn-primary focus:ring-2" />
                <span class="ml-2">Preserve original order (experimental)</span>
              </label>
            </div>
          </div>

          <label class="flex items-center cursor-pointer ml-4">
            <input type="checkbox" v-model="useKeyList"
              class="w-4 h-4 text-btn-primary bg-dark-input border-dark-border rounded focus:ring-btn-primary focus:ring-2" />
            <span class="ml-2">Use key list so B overrides A for specified keys</span>
          </label>

          <div v-if="useKeyList" class="my-2 ml-8">
            <label class="block mb-2 font-medium">Keys where B wins (one per line):</label>
            <p class="text-sm text-gray-400 mb-2">List object keys (e.g. event IDs, decision IDs) that your mod has
              added or changed. For these keys the output uses B’s version; all other keys use A’s.</p>
            <textarea v-model="customKeys" rows="4" placeholder="my_mod_event.0001&#10;my_mod_decision.0001"
              class="w-full px-3 py-2 bg-dark-input/80 border border-dark-border rounded-lg text-gray-200 focus:outline-none focus:ring-2 focus:ring-btn-primary/50 focus:border-btn-primary font-mono text-sm transition-all duration-200"></textarea>
          </div>
        </div>
      </div>

      <!-- File/Folder Selection -->
      <div class="mb-6">
        <label class="block mb-4 text-xl font-semibold">File/Folder Selection</label>
        <div class="my-4">
          <label class="block mb-2 font-medium">
            File Set A (Base):
            <span v-if="mergeSetA.length > 0" class="text-sm text-gray-400 ml-2">({{ mergeSetA.length }} {{
              mergeSetA.length === 1 ? 'item' : 'items' }})</span>
          </label>
          <p class="text-xs text-gray-500 mb-1">Wins by default. When updating a mod: put the latest vanilla/game files
            here.</p>
          <textarea :value="mergeSetA.join('\n')"
            @input="mergeSetA = $event.target.value.split('\n').filter(p => p.trim())" rows="3"
            placeholder="Select files or directories..."
            class="w-full px-3 py-2 bg-dark-input/80 border border-dark-border rounded-lg text-gray-200 placeholder:text-gray-400 focus:outline-none focus:ring-2 focus:ring-btn-primary/50 focus:border-btn-primary font-mono text-sm transition-all duration-200"></textarea>
          <div class="flex gap-2 mt-2">
            <button @click="selectFiles('Select Multiple Files To Merge (A)', '*.txt', 'mergeSetA')"
              class="btn-primary">Select File(s)</button>
            <button @click="selectFolder('Select Folder To Merge (A)', 'mergeSetA')" class="btn-primary">Select
              Folder</button>
            <button @click="clearSet('mergeSetA')" class="btn-secondary">Clear</button>
          </div>
        </div>

        <div class="mb-4">
          <label class="block mb-2 font-medium">
            File Set B (Mod):
            <span v-if="mergeSetB.length > 0" class="text-sm text-gray-400 ml-2">({{ mergeSetB.length }} {{
              mergeSetB.length === 1 ? 'item' : 'items' }})</span>
          </label>
          <p class="text-xs text-gray-500 mb-1">For keys in the key list, B’s version is used. When updating a mod: put
            your mod files here.</p>
          <textarea :value="mergeSetB.join('\n')"
            @input="mergeSetB = $event.target.value.split('\n').filter(p => p.trim())" rows="3"
            placeholder="Select files or directories..."
            class="w-full px-3 py-2 bg-dark-input/80 border border-dark-border rounded-lg text-gray-200 placeholder:text-gray-400 focus:outline-none focus:ring-2 focus:ring-btn-primary/50 focus:border-btn-primary font-mono text-sm transition-all duration-200"></textarea>
          <div class="flex gap-2 mt-2">
            <button @click="selectFiles('Select Multiple Files To Merge (B)', '*.txt', 'mergeSetB')"
              class="btn-primary">Select File(s)</button>
            <button @click="selectFolder('Select Folder To Merge (B)', 'mergeSetB')" class="btn-primary">Select
              Folder</button>
            <button @click="clearSet('mergeSetB')" class="btn-secondary">Clear</button>
          </div>
        </div>

        <div class="mb-4">
          <label class="block mb-2 font-medium">Output Directory:</label>
          <div class="flex gap-2">
            <input v-model="mergeOutputDir" type="text" placeholder="merger-output"
              class="flex-1 px-3 py-2 bg-dark-input/80 border border-dark-border rounded-lg text-gray-200 focus:outline-none focus:ring-2 focus:ring-btn-primary/50 focus:border-btn-primary transition-all duration-200" />
            <button @click="selectOutputDir" class="btn-primary">Browse</button>
          </div>
        </div>
      </div>

      <!-- Misc Options -->
      <div class="mb-4">
        <label class="block mb-4 text-xl font-semibold">Misc Options</label>
        <div class="my-4">
          <label class="block mb-2 font-medium">Custom Comment Prefix:</label>
          <input v-model="commentPrefix" type="text" placeholder="# MOD:"
            class="w-full px-3 py-2 bg-dark-input/80 border border-dark-border rounded-lg text-gray-200 focus:outline-none focus:ring-2 focus:ring-btn-primary/50 focus:border-btn-primary transition-all duration-200" />
          <p class="text-sm text-gray-400 mt-2 leading-relaxed">Comments with above prefix will be preserved during
            merger.</p>
        </div>
      </div>

      <button @click="runMerge" :disabled="merging"
        class="w-full mt-6 btn-primary disabled:opacity-50 disabled:cursor-not-allowed disabled:hover:shadow-material">
        {{ merging ? 'Merging...' : 'Run Merge' }}
      </button>
    </div>

    <!-- Results Panel (table-like, compact rows, lighter actions) -->
    <div v-if="mergeResults.length > 0"
      class="bg-dark-panel/60 backdrop-blur-sm rounded-xl p-4 sm:p-6 overflow-hidden shadow-material border border-dark-border/50 flex flex-col min-h-0">
      <h2 class="text-xl font-semibold mb-3 flex-shrink-0">Results ({{ mergeResults.length }})</h2>
      <div class="flex-1 min-h-0 overflow-auto border border-dark-border/50 rounded-lg">
        <table class="w-full border-collapse text-sm table-fixed">
          <colgroup>
            <col style="min-width: 14rem; width: 30%" />
            <col style="width: 4rem" />
            <col style="width: 4rem" />
            <col style="width: 4rem" />
            <col style="min-width: 5rem; width: 12%" />
            <col style="min-width: 11rem; width: 22%" />
          </colgroup>
          <thead class="sticky top-0 z-10 bg-dark-panel/95 border-b border-dark-border">
            <tr>
              <th class="text-left py-2 px-3 text-slate-400 font-medium">File</th>
              <th class="text-left py-2 px-2 text-slate-400 font-medium" title="Object keys replaced by B">Changed <span
                  class="text-gray-500 font-normal">(keys)</span></th>
              <th class="text-left py-2 px-2 text-slate-400 font-medium" title="Object keys from B not in A">Added <span
                  class="text-gray-500 font-normal">(keys)</span></th>
              <th class="text-left py-2 px-2 text-slate-400 font-medium" title="Object keys removed vs A">Removed <span
                  class="text-gray-500 font-normal">(keys)</span></th>
              <th class="text-left py-2 px-3 text-slate-400 font-medium">Output</th>
              <th class="text-left py-2 px-3 text-slate-400 font-medium">Diffs vs output</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(result, index) in mergeResults" :key="index"
              :class="index % 2 === 0 ? 'bg-dark-input/40' : 'bg-dark-input/20'"
              class="border-b border-dark-border/30 hover:bg-dark-input transition-colors">
              <td class="py-1.5 px-3 text-gray-200 font-mono truncate" :title="result.filePath">{{ result.filePath }}
              </td>
              <td class="py-1.5 px-2 text-gray-400 tabular-nums">{{ result.changed ?? 0 }}</td>
              <td class="py-1.5 px-2 tabular-nums" :class="result.added > 0 ? 'text-accent-success' : 'text-gray-400'">
                {{ result.added ?? 0 }}</td>
              <td class="py-1.5 px-2 text-gray-400 tabular-nums">{{ result.removed ?? 0 }}</td>
              <td class="py-1.5 px-3 text-gray-300 truncate" :title="result.outputPath">{{
                getFileName(result.outputPath) }}</td>
              <td class="py-1.5 px-3">
                <span class="inline-flex gap-2 flex-wrap">
                  <button type="button" @click="viewDiff(index, result.fileAPath, result.outputPath)" class="btn-accent"
                    title="Diff: File A vs merge output">
                    A vs Output
                  </button>
                  <button type="button" @click="viewDiff(index, result.fileBPath, result.outputPath)" class="btn-accent"
                    title="Diff: File B vs merge output">
                    B vs Output
                  </button>
                </span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- Diff Overlay -->
    <DiffViewer :visible="diffFilePath !== null" :lines="diffLines" :loading="loadingDiff" @close="closeDiff" />
  </div>
</template>

<script>
import { SelectDirectory, SelectMultipleFiles } from '../../bindings/paradox-modding-tool/fileservice.js'
import { GetDiff } from '../../bindings/paradox-modding-tool/diffservice.js'
import { MergeMultipleFileSets } from '../../bindings/paradox-modding-tool/mergerservice.js'
import DiffViewer from './DiffViewer.vue'

export default {
  name: 'MergeTool',
  components: { DiffViewer },
  data() {
    return {
      mergeSetA: [],
      mergeSetB: [],
      addAdditionalEntries: true,
      entryPlacement: 'bottom',
      useKeyList: false,
      customKeys: '',
      commentPrefix: '',
      mergeOutputDir: '',
      merging: false,
      mergeResults: [],
      diffLines: [],
      diffFilePath: null,
      loadingDiff: false
    }
  },
  methods: {
    clearState() {
      this.mergeSetA = []
      this.mergeSetB = []
      this.mergeOutputDir = ''
      this.mergeResults = []
      this.addAdditionalEntries = true
      this.entryPlacement = 'bottom'
      this.useKeyList = false
      this.customKeys = ''
      this.commentPrefix = ''
      this.diffLines = []
      this.diffFilePath = null
    },
    getFileName(path) {
      if (!path) return ''
      const parts = path.split(/[/\\]/)
      return parts[parts.length - 1] || path
    },
    async selectFiles(dialogTitle, filter, fileSet) {
      try {
        const selected = await SelectMultipleFiles(dialogTitle, filter)
        if (selected && selected.length > 0) {
          const existing = new Set(this[fileSet])
          for (const file of selected) {
            if (!existing.has(file)) this[fileSet].push(file)
          }
        }
      } catch (e) {
        alert('Error selecting files: ' + e)
      }
    },
    async selectFolder(dialogTitle, fileSet) {
      try {
        const selected = await SelectDirectory(dialogTitle)
        if (selected) this[fileSet].push(selected)
      } catch (e) {
        alert('Error selecting folder: ' + e)
      }
    },
    clearSet(fileSet) {
      this[fileSet] = []
    },
    async selectOutputDir() {
      try {
        const selected = await SelectDirectory('Select Output Directory')
        if (selected) this.mergeOutputDir = selected
      } catch (e) {
        alert('Error selecting directory: ' + e)
      }
    },
    async runMerge() {
      if (this.mergeSetA.length === 0 || this.mergeSetB.length === 0) {
        alert('Please select at least one file or directory for both sets')
        return
      }
      if (!this.mergeOutputDir) {
        alert('Please select an output directory')
        return
      }
      this.merging = true
      this.mergeResults = []
      try {
        const keys = this.useKeyList ? this.customKeys.split('\n').filter(k => k.trim() !== '') : []
        const results = await MergeMultipleFileSets(this.mergeSetA, this.mergeSetB, this.mergeOutputDir, {
          addAdditionalEntries: this.addAdditionalEntries,
          entryPlacement: this.entryPlacement,
          keyList: keys,
          customCommentPrefix: this.commentPrefix
        })
        this.mergeResults = results || []
        if (this.mergeResults.length === 0) {
          alert('No matching files found between the two sets. Make sure both sets contain files with matching names or paths.')
        }
      } catch (e) {
        alert('Error during merge: ' + e)
      } finally {
        this.merging = false
      }
    },
    async viewDiff(resultIndex, beforePath, afterPath) {
      const result = this.mergeResults[resultIndex]
      if (!result || result.error) {
        alert('Cannot view diff: ' + (result?.error || 'Unknown error'))
        return
      }
      this.loadingDiff = true
      this.diffFilePath = afterPath
      try {
        this.diffLines = (await GetDiff(beforePath, afterPath)) || []
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
    }
  }
}
</script>
