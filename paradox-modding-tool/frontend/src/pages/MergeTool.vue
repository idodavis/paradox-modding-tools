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
                A = vanilla (game install path). B = your mod. Output = directory for merged files.
              </p>
              <details
                class="group rounded-lg border border-(--p-surface-200) dark:border-(--p-surface-700) overflow-hidden mb-4">
                <summary class="px-3 py-2 cursor-pointer text-sm flex items-center justify-between gap-2">
                  <span class="font-medium">Merge options</span>
                  <span class="group-open:rotate-180 transition-transform">▾</span>
                </summary>
                <div
                  class="px-3 py-3 text-sm border-t border-(--p-surface-200) dark:border-(--p-surface-700) space-y-3">
                  <p><strong>A (base)</strong> wins unless the key is in the key list; those use <strong>B</strong>.</p>
                  <div class="flex items-center gap-2">
                    <Checkbox v-model="addAdditionalEntries" inputId="vanilla_addAdditionalEntries" binary />
                    <label for="vanilla_addAdditionalEntries">Add entries from B that don't exist in A</label>
                  </div>
                  <div v-if="addAdditionalEntries" class="ml-4 space-y-2">
                    <div class="flex items-center gap-2">
                      <RadioButton v-model="entryPlacement" inputId="vanilla_entryBottom" name="vanilla_entryPlacement"
                        value="bottom" />
                      <label for="vanilla_entryBottom">Bottom of file</label>
                    </div>
                    <div class="flex items-center gap-2">
                      <RadioButton v-model="entryPlacement" inputId="vanilla_entryPreserve"
                        name="vanilla_entryPlacement" value="preserve_order" />
                      <label for="vanilla_entryPreserve">Preserve order (experimental)</label>
                    </div>
                  </div>
                  <div class="flex items-center gap-2">
                    <Checkbox v-model="useKeyList" inputId="vanilla_useKeyList" binary />
                    <label for="vanilla_useKeyList">Use key list (B overrides A for listed keys)</label>
                  </div>
                  <div v-if="useKeyList" class="ml-4">
                    <label class="block mb-1 font-medium text-sm">Keys where B wins (one per line):</label>
                    <Textarea v-model="customKeys" rows="3" placeholder="my_mod_event.0001" class="w-full" />
                  </div>
                  <div>
                    <label class="block mb-1 font-medium text-sm">Custom comment prefix:</label>
                    <Textarea v-model="commentPrefix" rows="1" placeholder="# MOD:" class="w-full" />
                  </div>
                </div>
              </details>
              <div class="mb-4">
                <label class="block text-sm font-medium mb-2">Vanilla (A):</label>
                <InputText :model-value="gameInstallPath || ''" readonly class="w-full"
                  placeholder="Set game install path in Modding Docs or header" />
                <span class="text-xs text-(--p-surface-500)">Game: {{ game.toUpperCase() }}</span>
              </div>
              <FileSelector v-model="mergeModPaths" label="Mod (B)" class="mb-4"
                file-dialog-title="Select Mod Files (B)" folder-dialog-title="Select Mod Folder (B)" />
              <div class="mb-4">
                <label class="block mb-2 font-medium text-sm">Output Directory:</label>
                <InputText v-model="mergeOutputDir" class="w-full min-w-0" placeholder="merger-output" />
                <Button label="Browse" size="small" class="mt-2" @click="selectOutputDir" />
              </div>
              <Button :label="merging ? 'Merging...' : 'Run Merge'"
                :disabled="merging || !gameInstallPath || mergeModPaths.length === 0 || !mergeOutputDir"
                :loading="merging" @click="runVanillaMerge" />
            </template>
          </Card>
          <Card v-if="mergeResults.length > 0" class="flex flex-col overflow-hidden">
            <template #content>
              <DataTable :value="mergeResults" data-key="filePath" class="flex-1">
                <Column field="filePath" header="File" />
                <Column field="outputPath" header="Output">
                  <template #body="{ data }">
                    <span class="block truncate" :title="data.outputPath">{{ baseName(data.outputPath) }}</span>
                  </template>
                </Column>
                <Column header="Diffs">
                  <template #body="{ data }">
                    <div class="flex gap-2">
                      <Button label="A vs Output" severity="info" variant="outlined"
                        @click="viewDiff(data.fileAPath, data.outputPath)" />
                      <Button label="B vs Output" severity="success" variant="outlined"
                        @click="viewDiff(data.fileBPath, data.outputPath)" />
                    </div>
                  </template>
                </Column>
              </DataTable>
            </template>
          </Card>
        </TabPanel>

        <!-- Mode 2: Two sets / directories -->
        <TabPanel value="sets" class="flex flex-col gap-4">
          <Card>
            <template #content>
              <details
                class="group rounded-lg border border-(--p-surface-200) dark:border-(--p-surface-700) overflow-hidden mb-4">
                <summary class="px-3 py-2 cursor-pointer text-sm flex items-center justify-between gap-2">
                  <span class="font-medium">Merge options</span>
                  <span class="group-open:rotate-180 transition-transform">▾</span>
                </summary>
                <div
                  class="px-3 py-3 text-sm border-t border-(--p-surface-200) dark:border-(--p-surface-700) space-y-3">
                  <p><strong>A (base)</strong> wins unless the key is in the key list; those use <strong>B</strong>.</p>
                  <div class="flex items-center gap-2">
                    <Checkbox v-model="addAdditionalEntries" inputId="sets_addAdditionalEntries" binary />
                    <label for="sets_addAdditionalEntries">Add entries from B that don't exist in A</label>
                  </div>
                  <div v-if="addAdditionalEntries" class="ml-4 space-y-2">
                    <div class="flex items-center gap-2">
                      <RadioButton v-model="entryPlacement" inputId="sets_entryBottom" name="sets_entryPlacement"
                        value="bottom" />
                      <label for="sets_entryBottom">Bottom of file</label>
                    </div>
                    <div class="flex items-center gap-2">
                      <RadioButton v-model="entryPlacement" inputId="sets_entryPreserve" name="sets_entryPlacement"
                        value="preserve_order" />
                      <label for="sets_entryPreserve">Preserve order (experimental)</label>
                    </div>
                  </div>
                  <div class="flex items-center gap-2">
                    <Checkbox v-model="useKeyList" inputId="sets_useKeyList" binary />
                    <label for="sets_useKeyList">Use key list (B overrides A for listed keys)</label>
                  </div>
                  <div v-if="useKeyList" class="ml-4">
                    <label class="block mb-1 font-medium text-sm">Keys where B wins (one per line):</label>
                    <Textarea v-model="customKeys" rows="3" placeholder="my_mod_event.0001" class="w-full" />
                  </div>
                  <div>
                    <label class="block mb-1 font-medium text-sm">Custom comment prefix:</label>
                    <Textarea v-model="commentPrefix" rows="1" placeholder="# MOD:" class="w-full" />
                  </div>
                </div>
              </details>
              <FileSelector v-model="pathsA" label="File Set A (Base)" class="mb-4"
                file-dialog-title="Select Multiple Files To Merge (A)" folder-dialog-title="Select Folder To Merge (A)"
                hint="Wins by default. When updating a mod: put the latest vanilla/game files here." />
              <FileSelector v-model="pathsB" label="File Set B (Mod)" class="mb-4"
                file-dialog-title="Select Multiple Files To Merge (B)" folder-dialog-title="Select Folder To Merge (B)"
                hint="For keys in the key list, B's version is used. Put your mod files here." />
              <div class="mb-4">
                <label class="block mb-2 font-medium text-sm">Output Directory:</label>
                <InputText v-model="mergeOutputDir" class="w-full min-w-0" placeholder="merger-output" />
                <Button label="Browse" size="small" class="mt-2" @click="selectOutputDir" />
              </div>
              <Button :label="merging ? 'Merging...' : 'Run Merge'"
                :disabled="merging || !pathsA.length || !pathsB.length || !mergeOutputDir" :loading="merging"
                @click="runMerge" />
            </template>
          </Card>
          <Card v-if="mergeResults.length > 0" class="flex flex-col overflow-hidden">
            <template #content>
              <DataTable :value="mergeResults" data-key="filePath" class="flex-1">
                <Column field="filePath" header="File" />
                <Column field="outputPath" header="Output">
                  <template #body="{ data }">
                    <span class="block truncate" :title="data.outputPath">{{ baseName(data.outputPath) }}</span>
                  </template>
                </Column>
                <Column header="Diffs">
                  <template #body="{ data }">
                    <div class="flex gap-2">
                      <Button label="A vs Output" severity="info" variant="outlined"
                        @click="viewDiff(data.fileAPath, data.outputPath)" />
                      <Button label="B vs Output" severity="success" variant="outlined"
                        @click="viewDiff(data.fileBPath, data.outputPath)" />
                    </div>
                  </template>
                </Column>
              </DataTable>
            </template>
          </Card>
        </TabPanel>

        <!-- Mode 3: Any two files -->
        <TabPanel value="any" class="flex flex-col gap-4">
          <Card>
            <template #content>
              <details
                class="group rounded-lg border border-(--p-surface-200) dark:border-(--p-surface-700) overflow-hidden mb-4">
                <summary class="px-3 py-2 cursor-pointer text-sm flex items-center justify-between gap-2">
                  <span class="font-medium">Merge options</span>
                  <span class="group-open:rotate-180 transition-transform">▾</span>
                </summary>
                <div
                  class="px-3 py-3 text-sm border-t border-(--p-surface-200) dark:border-(--p-surface-700) space-y-3">
                  <p><strong>A (base)</strong> wins unless the key is in the key list; those use <strong>B</strong>.</p>
                  <div class="flex items-center gap-2">
                    <Checkbox v-model="addAdditionalEntries" inputId="any_addAdditionalEntries" binary />
                    <label for="any_addAdditionalEntries">Add entries from B that don't exist in A</label>
                  </div>
                  <div v-if="addAdditionalEntries" class="ml-4 space-y-2">
                    <div class="flex items-center gap-2">
                      <RadioButton v-model="entryPlacement" inputId="any_entryBottom" name="any_entryPlacement"
                        value="bottom" />
                      <label for="any_entryBottom">Bottom of file</label>
                    </div>
                    <div class="flex items-center gap-2">
                      <RadioButton v-model="entryPlacement" inputId="any_entryPreserve" name="any_entryPlacement"
                        value="preserve_order" />
                      <label for="any_entryPreserve">Preserve order (experimental)</label>
                    </div>
                  </div>
                  <div class="flex items-center gap-2">
                    <Checkbox v-model="useKeyList" inputId="any_useKeyList" binary />
                    <label for="any_useKeyList">Use key list (B overrides A for listed keys)</label>
                  </div>
                  <div v-if="useKeyList" class="ml-4">
                    <label class="block mb-1 font-medium text-sm">Keys where B wins (one per line):</label>
                    <Textarea v-model="customKeys" rows="3" placeholder="my_mod_event.0001" class="w-full" />
                  </div>
                  <div>
                    <label class="block mb-1 font-medium text-sm">Custom comment prefix:</label>
                    <Textarea v-model="commentPrefix" rows="1" placeholder="# MOD:" class="w-full" />
                  </div>
                </div>
              </details>
              <div class="flex flex-wrap gap-4 items-end mb-4">
                <div class="flex flex-col gap-1 min-w-[200px]">
                  <label class="text-sm font-medium">File A (base)</label>
                  <InputText :model-value="mergeFileA" readonly placeholder="Select file A" class="w-full" />
                  <Button label="Select file A" icon="pi pi-file" size="small" class="mt-1" @click="selectMergeFileA" />
                </div>
                <div class="flex flex-col gap-1 min-w-[200px]">
                  <label class="text-sm font-medium">File B (mod)</label>
                  <InputText :model-value="mergeFileB" readonly placeholder="Select file B" class="w-full" />
                  <Button label="Select file B" icon="pi pi-file" size="small" class="mt-1" @click="selectMergeFileB" />
                </div>
              </div>
              <Button :label="merging ? 'Merging...' : 'Merge and save…'"
                :disabled="merging || !mergeFileA || !mergeFileB" :loading="merging" @click="mergeTwoFilesAndSave" />
            </template>
          </Card>
        </TabPanel>
      </TabPanels>
    </Tabs>

    <DiffViewer :visible="diffFilePath !== null" :lines="diffLines" :loading="loadingDiff" @close="closeDiff" />
  </div>
</template>

<script setup>
import { ref } from 'vue'
import {
  SelectDirectory,
  GetScriptRoot,
  SelectSingleFile,
  SaveFile
} from '../../bindings/paradox-modding-tool/fileservice.js'
import { GetDiff } from '../../bindings/paradox-modding-tool/diffservice.js'
import { MergeMultipleFileSets, MergeTwoFiles } from '../../bindings/paradox-modding-tool/mergerservice.js'
import DiffViewer from '../components/DiffViewer.vue'
import FileSelector from '../components/FileSelector.vue'
import { useAppSettings } from '../composables/useAppSettings'
import { parsePathList } from '../utils/general.js'

const { game, gameInstallPath } = useAppSettings()

const activeMode = ref('sets')
const pathsA = ref([])
const pathsB = ref([])
const mergeModPaths = ref([])
const mergeFileA = ref('')
const mergeFileB = ref('')
const addAdditionalEntries = ref(true)
const entryPlacement = ref('bottom')
const useKeyList = ref(false)
const customKeys = ref('')
const commentPrefix = ref('')
const mergeOutputDir = ref('')
const merging = ref(false)
const mergeResults = ref([])
const diffLines = ref([])
const diffFilePath = ref(null)
const loadingDiff = ref(false)

async function selectOutputDir() {
  try {
    const selected = await SelectDirectory('Select Output Directory')
    if (selected) mergeOutputDir.value = selected
  } catch (e) {
    alert('Error selecting directory: ' + e)
  }
}

function mergerOptions() {
  return {
    addAdditionalEntries: addAdditionalEntries.value,
    entryPlacement: entryPlacement.value,
    keyList: useKeyList.value ? parsePathList(customKeys.value) : [],
    customCommentPrefix: commentPrefix.value
  }
}

async function runVanillaMerge() {
  const path = gameInstallPath.value?.trim()
  if (!path || mergeModPaths.value.length === 0 || !mergeOutputDir.value) {
    mergeResults.value = []
    return
  }
  merging.value = true
  mergeResults.value = []
  try {
    const root = await GetScriptRoot(path, game.value)
    const res = await MergeMultipleFileSets([root], mergeModPaths.value, mergeOutputDir.value, mergerOptions())
    mergeResults.value = res || []
    if (!mergeResults.value.length) alert('No matching files between vanilla and mod.')
  } catch (e) {
    alert('Error during merge: ' + e)
  } finally {
    merging.value = false
  }
}

async function runMerge() {
  if (!pathsA.value.length || !pathsB.value.length || !mergeOutputDir.value) {
    mergeResults.value = []
    return
  }
  merging.value = true
  mergeResults.value = []
  try {
    const res = await MergeMultipleFileSets(pathsA.value, pathsB.value, mergeOutputDir.value, mergerOptions())
    mergeResults.value = res || []
    if (!mergeResults.value.length) alert('No matching files between the two sets.')
  } catch (e) {
    alert('Error during merge: ' + e)
  } finally {
    merging.value = false
  }
}

async function selectMergeFileA() {
  try {
    const path = await SelectSingleFile('Select file A (base)', '*.txt')
    if (path) mergeFileA.value = path
  } catch (e) {
    alert('Error: ' + (e?.message ?? e))
  }
}

async function selectMergeFileB() {
  try {
    const path = await SelectSingleFile('Select file B (mod)', '*.txt')
    if (path) mergeFileB.value = path
  } catch (e) {
    alert('Error: ' + (e?.message ?? e))
  }
}

async function mergeTwoFilesAndSave() {
  if (!mergeFileA.value || !mergeFileB.value) return
  merging.value = true
  try {
    const content = await MergeTwoFiles(mergeFileA.value, mergeFileB.value, mergerOptions())
    const defaultName = 'merged.txt'
    const saved = await SaveFile(defaultName, 'txt', content)
    if (saved) alert('Saved to: ' + saved)
  } catch (e) {
    alert('Error during merge: ' + e)
  } finally {
    merging.value = false
  }
}

async function viewDiff(beforePath, afterPath) {
  if (!beforePath || !afterPath) return
  if (beforePath === afterPath) {
    alert('Output was written over this input file. Use an output directory different from your input folders.')
    return
  }
  loadingDiff.value = true
  diffFilePath.value = afterPath
  try {
    diffLines.value = (await GetDiff(beforePath, afterPath)) || []
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
}

function baseName(path) {
  return path ? path.split(/[/\\]/).pop() || path : ''
}
</script>
