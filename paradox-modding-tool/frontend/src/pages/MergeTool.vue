<template>
  <div class="flex-1 flex flex-col min-h-0 min-w-0 p-4 overflow-auto">
    <!-- Input Panel (InventoryTool-style) -->
    <div class="w-full max-w-full rounded-xl p-4 border border-dark-border mb-4">
      <h2 class="text-lg font-semibold mb-4">Merge Tool</h2>

      <!-- Merge Options (collapsed by default) -->
      <div class="mb-4">
        <details class="group rounded-lg border border-dark-border/50 overflow-hidden">
          <summary
            class="px-3 py-2 cursor-pointer text-sm text-gray-400 hover:bg-dark-border/50 flex items-center justify-between gap-2">
            <span class="font-medium">Merge options</span>
            <span class="group-open:rotate-180 transition-transform">▾</span>
          </summary>
          <div class="px-3 py-3 text-sm text-gray-400 border-t border-dark-border/50 space-y-3">
            <p><strong>A (base)</strong> wins unless the key is in the key list; those use <strong>B</strong>. After a game update: put <em>vanilla</em> in A, <em>mod</em> in B.</p>
            <div class="flex items-center gap-2">
              <Checkbox v-model="addAdditionalEntries" inputId="addAdditionalEntries" binary />
              <label class="cursor-pointer" for="addAdditionalEntries">Add entries from B that don't exist in A</label>
            </div>
            <div v-if="addAdditionalEntries" class="ml-4 space-y-2">
              <div class="flex items-center gap-2">
                <RadioButton v-model="entryPlacement" inputId="entryPlacementBottom" name="entryPlacement" value="bottom" />
                <label class="cursor-pointer" for="entryPlacementBottom">Bottom of file</label>
              </div>
              <div class="flex items-center gap-2">
                <RadioButton v-model="entryPlacement" inputId="entryPlacementPreserve" name="entryPlacement" value="preserve_order" />
                <label class="cursor-pointer" for="entryPlacementPreserve">Preserve order (experimental)</label>
              </div>
            </div>
            <div class="flex items-center gap-2">
              <Checkbox v-model="useKeyList" inputId="useKeyList" binary />
              <label class="cursor-pointer" for="useKeyList">Use key list (B overrides A for listed keys)</label>
            </div>
            <div v-if="useKeyList" class="ml-4">
              <label class="block mb-1 font-medium text-sm">Keys where B wins (one per line):</label>
              <Textarea v-model="customKeys" rows="3" placeholder="my_mod_event.0001" class="w-full px-3 py-2" />
            </div>
            <div>
              <label class="block mb-1 font-medium text-sm">Custom comment prefix:</label>
              <Textarea v-model="commentPrefix" rows="1" placeholder="# MOD:" class="w-full px-3 py-2" />
            </div>
          </div>
        </details>
      </div>

      <!-- File/Folder Selection -->
      <div class="mb-4">
        <FileSelector v-model="pathsA" label="File Set A (Base)" class="mb-4"
          file-dialog-title="Select Multiple Files To Merge (A)"
          folder-dialog-title="Select Folder To Merge (A)"
          hint="Wins by default. When updating a mod: put the latest vanilla/game files here." />
        <FileSelector v-model="pathsB" label="File Set B (Mod)"
          file-dialog-title="Select Multiple Files To Merge (B)"
          folder-dialog-title="Select Folder To Merge (B)"
          hint="For keys in the key list, B's version is used. Put your mod files here." />
      </div>

      <div class="mb-4">
        <label class="block mb-2 font-medium text-sm">Output Directory:</label>
        <Textarea v-model="mergeOutputDir" rows="1" placeholder="merger-output" class="w-full min-w-0 px-3 py-2" />
        <Button label="Browse" size="small" class="mt-2" @click="selectOutputDir" />
      </div>

      <Button @click="runMerge" :disabled="merging || !pathsA.length || !pathsB.length || !mergeOutputDir"
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
import { SelectDirectory } from '../../bindings/paradox-modding-tool/fileservice.js'
import { GetDiff } from '../../bindings/paradox-modding-tool/diffservice.js'
import { MergeMultipleFileSets } from '../../bindings/paradox-modding-tool/mergerservice.js'
import DiffViewer from '../components/DiffViewer.vue'
import FileSelector from '../components/FileSelector.vue'
import DataTable from 'primevue/datatable'
import Button from 'primevue/button'
import Checkbox from 'primevue/checkbox'
import Column from 'primevue/column'
import RadioButton from 'primevue/radiobutton'
import Textarea from 'primevue/textarea'
import { parsePathList } from '../utils/paths.js'

export default {
  name: 'MergeTool',
  components: {
    DiffViewer,
    FileSelector,
    DataTable,
    Button,
    Checkbox,
    Column,
    RadioButton,
    Textarea
  },
  data() {
    return {
      pathsA: [],
      pathsB: [],
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
    baseName(path) { return path ? path.split(/[/\\]/).pop() || path : '' },
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
        const keys = this.useKeyList ? parsePathList(this.customKeys) : []
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
