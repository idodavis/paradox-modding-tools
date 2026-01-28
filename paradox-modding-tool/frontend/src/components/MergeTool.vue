<template>
  <div class="flex-1 grid grid-cols-1 lg:grid-cols-2 gap-6 p-4 sm:p-6 overflow-auto bg-dark-input">
    <!-- Configuration Panel -->
    <div
      class="bg-dark-panel/60 backdrop-blur-sm rounded-xl p-4 sm:p-6 overflow-y-auto shadow-material border border-dark-border/50">
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
                <strong>B</strong>.
              </p>
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
            <span v-if="pathsA.length" class="text-sm text-gray-400 ml-2">({{ pathsA.length }} {{ pathsA.length === 1 ?
              'item' : 'items' }})</span>
          </label>
          <p class="text-xs text-gray-500 mb-1">Wins by default. When updating a mod: put the latest vanilla/game files
            here.</p>
          <textarea v-model="pathsAText" rows="3" placeholder="Select files or directories..."
            class="w-full px-3 py-2 bg-dark-input/80 border border-dark-border rounded-lg text-gray-200 placeholder:text-gray-400 focus:outline-none focus:ring-2 focus:ring-btn-primary/50 focus:border-btn-primary font-mono text-sm transition-all duration-200"></textarea>
          <div class="flex gap-2 mt-2">
            <button @click="selectPaths('A', 'pathsAText')" class="btn-primary">Select File(s)</button>
            <button @click="selectFolderPath('pathsAText')" class="btn-primary">Select Folder</button>
            <button @click="pathsAText = ''" class="btn-secondary">Clear</button>
          </div>
        </div>

        <div class="mb-4">
          <label class="block mb-2 font-medium">
            File Set B (Mod):
            <span v-if="pathsB.length" class="text-sm text-gray-400 ml-2">({{ pathsB.length }} {{ pathsB.length === 1 ?
              'item' : 'items' }})</span>
          </label>
          <p class="text-xs text-gray-500 mb-1">For keys in the key list, B’s version is used. When updating a mod: put
            your mod files here.</p>
          <textarea v-model="pathsBText" rows="3" placeholder="Select files or directories..."
            class="w-full px-3 py-2 bg-dark-input/80 border border-dark-border rounded-lg text-gray-200 placeholder:text-gray-400 focus:outline-none focus:ring-2 focus:ring-btn-primary/50 focus:border-btn-primary font-mono text-sm transition-all duration-200"></textarea>
          <div class="flex gap-2 mt-2">
            <button @click="selectPaths('B', 'pathsBText')" class="btn-primary">Select File(s)</button>
            <button @click="selectFolderPath('pathsBText')" class="btn-primary">Select Folder</button>
            <button @click="pathsBText = ''" class="btn-secondary">Clear</button>
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

    <!-- Results -->
    <div v-if="mergeResults.length > 0"
      class="bg-dark-panel/60 backdrop-blur-sm rounded-xl p-4 sm:p-6 overflow-hidden shadow-material border border-dark-border/50 flex flex-col min-h-0">
      <h2 class="text-xl font-semibold mb-3 flex-shrink-0">Results ({{ mergeResults.length }})</h2>
      <div class="flex-1 min-h-0 overflow-auto border border-dark-border/50 rounded-lg">
        <table class="w-full border-collapse text-sm">
          <colgroup>
            <col style="min-width: 10rem" />
            <col style="min-width: 5rem" />
            <col style="min-width: 13rem; width: 1%" />
          </colgroup>
          <thead class="sticky top-0 z-10 bg-dark-panel/95 border-b border-dark-border">
            <tr>
              <th class="text-left py-2 px-3 text-slate-400 font-medium">File</th>
              <th class="text-left py-2 px-3 text-slate-400 font-medium">Output</th>
              <th class="text-right py-2 px-3 text-slate-400 font-medium">Diffs</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(r, i) in mergeResults" :key="i"
              :class="[i % 2 ? 'bg-dark-input/20' : 'bg-dark-input/40', 'border-b border-dark-border/30 hover:bg-dark-input']">
              <td class="py-1.5 px-3 text-gray-200 font-mono truncate" :title="r.filePath">{{ r.filePath }}</td>
              <td class="py-1.5 px-3 text-gray-300 truncate" :title="r.outputPath">{{ baseName(r.outputPath) }}</td>
              <td class="py-1.5 px-3 text-right whitespace-nowrap">
                <button type="button" @click="viewDiff(r.fileAPath, r.outputPath)" class="btn-accent text-sm mr-1.5">A
                  vs Output</button>
                <button type="button" @click="viewDiff(r.fileBPath, r.outputPath)" class="btn-accent text-sm">B vs
                  Output</button>
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
      pathsAText: '',
      pathsBText: '',
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
  computed: {
    pathsA() { return (this.pathsAText || '').split(/\r?\n/).map(p => p.trim()).filter(Boolean) },
    pathsB() { return (this.pathsBText || '').split(/\r?\n/).map(p => p.trim()).filter(Boolean) }
  },
  methods: {
    baseName(path) { return path ? path.split(/[/\\]/).pop() || path : '' },
    async selectPaths(label, target) {
      try {
        const selected = await SelectMultipleFiles('Select Multiple Files To Merge (' + label + ')', '*.txt')
        if (selected?.length) {
          const list = [...new Set((target === 'pathsAText' ? this.pathsA : this.pathsB).concat(selected))]
          this[target] = list.join('\n')
        }
      } catch (e) { alert('Error selecting files: ' + e) }
    },
    async selectFolderPath(target) {
      try {
        const sel = await SelectDirectory('Select Folder To Merge')
        if (sel) {
          const list = target === 'pathsAText' ? this.pathsA : this.pathsB
          if (!list.includes(sel)) this[target] = (this[target] ? this[target] + '\n' : '') + sel
        }
      } catch (e) { alert('Error selecting folder: ' + e) }
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
      if (!this.pathsA.length || !this.pathsB.length) { alert('Select at least one file or folder for both sets'); return }
      if (!this.mergeOutputDir) { alert('Select an output directory'); return }
      this.merging = true
      this.mergeResults = []
      try {
        const keys = this.useKeyList ? this.customKeys.split('\n').map(k => k.trim()).filter(Boolean) : []
        const res = await MergeMultipleFileSets(this.pathsA, this.pathsB, this.mergeOutputDir, {
          addAdditionalEntries: this.addAdditionalEntries,
          entryPlacement: this.entryPlacement,
          keyList: keys,
          customCommentPrefix: this.commentPrefix
        })
        this.mergeResults = res || []
        if (!this.mergeResults.length) alert('No matching files between the two sets.')
      } catch (e) { alert('Error during merge: ' + e) }
      finally { this.merging = false }
    },
    async viewDiff(beforePath, afterPath) {
      if (!beforePath || !afterPath) return
      if (beforePath === afterPath) {
        alert('Output was written over this input file, so there is no diff. Use an output directory different from your input folders.')
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
