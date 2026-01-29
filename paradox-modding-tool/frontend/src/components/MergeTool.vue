<template>
  <div class="flex-1 grid grid-cols-1 gap-6 p-4 overflow-auto">
    <!-- Configuration Panel -->
    <div class="backdrop-blur-sm rounded-xl p-4 overflow-y-auto border border-dark-border">
      <!-- Merge Options -->
      <div class="mb-10">
        <div class="mb-4">
          <h3 class="text-lg font-semibold mb-3">Merge Options</h3>

          <details class="mb-5 group rounded-lg border border-dark-border/50 overflow-hidden">
            <summary
              class="px-3 py-2 cursor-pointer text-sm text-gray-400 hover:bg-dark-border/50 flex items-center justify-between gap-2">
              <span class="text-gray-400 font-medium">How precedence works</span>
              <span class="text-gray-400 group-open:rotate-180 transition-transform">▾</span>
            </summary>
            <div class="px-3 py-3 text-sm text-gray-400 border-t border-dark-border/50">
              <p><strong>A (base)</strong> wins for every key unless it’s in the <strong>key list</strong>; those use
                <strong>B</strong>.
              </p>
              <p class="text-gray-400">After a game update: put <em>vanilla</em> in A, <em>mod</em> in B, and list your
                mod’s object keys (events, decisions, etc.). Output = vanilla except where you changed things.</p>
            </div>
          </details>

          <div class="flex items-center ml-4">
            <Checkbox v-model="addAdditionalEntries" inputId="addAdditionalEntries" binary />
            <label class="ml-2 cursor-pointer" for="addAdditionalEntries">Add entries from B that don’t exist in A (e.g.
              mod-only events)</label>
          </div>

          <div v-if="addAdditionalEntries" class="mb-4 ml-8">
            <div class="my-2 text-sm text-gray-400">Additional Entry Placement:</div>
            <div class="space-y-2">
              <div class="flex items-center">
                <RadioButton v-model="entryPlacement" inputId="entryPlacementBottom" name="entryPlacement"
                  value="bottom" />
                <label class="ml-2 cursor-pointer" for="entryPlacementBottom">Bottom of file (with sectional
                  comment)</label>
              </div>
              <div class="flex items-center">
                <RadioButton v-model="entryPlacement" inputId="entryPlacementPreserve" name="entryPlacement"
                  value="preserve_order" />
                <label class="ml-2 cursor-pointer" for="entryPlacementPreserve">Preserve original order
                  (experimental)</label>
              </div>
            </div>
          </div>

          <div class="flex items-center ml-4">
            <Checkbox v-model="useKeyList" inputId="useKeyList" binary />
            <label class="ml-2 cursor-pointer" for="useKeyList">Use key list so B overrides A for specified
              keys</label>
          </div>

          <div v-if="useKeyList" class="my-2 ml-8">
            <label class="block mb-2 font-medium">Keys where B wins (one per line):</label>
            <p class="text-sm text-gray-400 mb-2">List object keys (e.g. event IDs, decision IDs) that your mod has
              added or changed. For these keys the output uses B's version; all other keys use A's.</p>
            <Textarea v-model="customKeys" rows="4" placeholder="my_mod_event.0001&#10;my_mod_decision.0001"
              class="w-full px-3 py-2" />
          </div>
        </div>
      </div>

      <!-- File/Folder Selection -->
      <div class="mb-10">
        <label class="block mb-4 text-xl font-semibold">File/Folder Selection</label>
        <div class="mb-8">
          <label class="block mb-2 font-medium">
            File Set A (Base):
            <span v-if="pathsA.length" class="text-sm text-gray-400 ml-2">({{ pathsA.length }} {{ pathsA.length === 1 ?
              'item' : 'items' }})</span>
          </label>
          <p class="text-xs text-gray-500 mb-1">Wins by default. When updating a mod: put the latest vanilla/game files
            here.</p>
          <Textarea v-model="pathsAText" rows="3" placeholder="Select files or directories..."
            class="w-full px-3 py-2" />
          <div class="flex gap-2 mt-2">
            <Button label="Select File(s)" @click="selectPaths('A', 'pathsAText')" />
            <Button label="Select Folder" @click="selectFolderPath('pathsAText')" />
            <Button label="Clear" severity="secondary" @click="pathsAText = ''" />
          </div>
        </div>

        <div class="mb-8">
          <label class="block mb-2 font-medium">
            File Set B (Mod):
            <span v-if="pathsB.length" class="text-sm text-gray-400 ml-2">({{ pathsB.length }} {{ pathsB.length === 1 ?
              'item' : 'items' }})</span>
          </label>
          <p class="text-xs text-gray-500 mb-1">For keys in the key list, B's version is used. When updating a mod: put
            your mod files here.</p>
          <Textarea v-model="pathsBText" rows="3" placeholder="Select files or directories..."
            class="w-full px-3 py-2" />
          <div class="flex gap-2 mt-2">
            <Button label="Select File(s)" @click="selectPaths('B', 'pathsBText')" />
            <Button label="Select Folder" @click="selectFolderPath('pathsBText')" />
            <Button label="Clear" severity="secondary" @click="pathsBText = ''" />
          </div>
        </div>

        <div>
          <label class="block mb-2 font-medium">Output Directory:</label>
          <Textarea v-model="mergeOutputDir" rows="1" placeholder="merger-output" class="w-full px-3 py-2" />
          <Button label="Browse" @click="selectOutputDir" />
        </div>
      </div>

      <!-- Misc Options -->
      <div class="mb-4">
        <label class="block mb-4 text-xl font-semibold">Misc Options</label>
        <div class="my-4">
          <label class="block mb-2 font-medium">Custom Comment Prefix:</label>
          <Textarea v-model="commentPrefix" rows="1" placeholder="# MOD:" class="w-full px-3 py-2" />
          <p class="text-sm text-gray-400 mt-2 leading-relaxed">Comments with above prefix will be preserved during
            merger.</p>
        </div>
      </div>

      <Button @click="runMerge" :disabled="merging"
        class="w-full mt-6 btn-primary disabled:opacity-50 disabled:cursor-not-allowed"
        :label="merging ? 'Merging...' : 'Run Merge'" />
    </div>

    <!-- Results -->
    <DataTable v-if="mergeResults.length > 0" :value="mergeResults" dataKey="filePath"
      :title="`Results (${mergeResults.length})`" panelClass="overflow-hidden flex flex-col min-h-0">
      <Column field="filePath" header="File" bodyClass="text-gray-200 font-mono">
        <template #body="{ data }">
          <span class="block truncate" :title="data.filePath">{{ data.filePath }}</span>
        </template>
      </Column>
      <Column field="outputPath" header="Output" bodyClass="text-gray-300">
        <template #body="{ data }">
          <span class="block truncate" :title="data.outputPath">{{ baseName(data.outputPath) }}</span>
        </template>
      </Column>
      <Column header="Diffs" bodyClass="text-right">
        <template #body="{ data }">
          <div class="flex justify-end gap-2">
            <Button label="A vs Output" severity="info" variant="outlined"
              @click="viewDiff(data.fileAPath, data.outputPath)" />
            <Button label="B vs Output" severity="success" variant="outlined"
              @click="viewDiff(data.fileBPath, data.outputPath)" />
          </div>
        </template>
      </Column>
    </DataTable>

    <!-- Diff Overlay -->
    <DiffViewer :visible="diffFilePath !== null" :lines="diffLines" :loading="loadingDiff" @close="closeDiff" />
  </div>
</template>

<script>
import { SelectDirectory, SelectMultipleFiles } from '../../bindings/paradox-modding-tool/fileservice.js'
import { GetDiff } from '../../bindings/paradox-modding-tool/diffservice.js'
import { MergeMultipleFileSets } from '../../bindings/paradox-modding-tool/mergerservice.js'
import DiffViewer from './DiffViewer.vue'
import DataTable from 'primevue/datatable'
import Button from 'primevue/button'
import Checkbox from 'primevue/checkbox'
import Column from 'primevue/column'
import RadioButton from 'primevue/radiobutton'
import Textarea from 'primevue/textarea'

export default {
  name: 'MergeTool',
  components: {
    DiffViewer,
    DataTable,
    Button,
    Checkbox,
    Column,
    RadioButton,
    Textarea
  },
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
