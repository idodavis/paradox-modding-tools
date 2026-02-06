<template>
  <div class="flex-1 flex flex-col min-h-0 min-w-0 p-4 overflow-auto">
    <div class="max-w-2xl mx-auto w-full pt-6">
      <h2 class="text-xl font-semibold mb-4">Settings</h2>
      <Card class="mb-4">
        <template #title>Game install directories</template>
        <template #content>
          <p class="text-(--p-surface-600) dark:text-(--p-surface-400) text-sm mb-4">
            Set the install path for each game. These are used by Modding Docs, Compare (vanilla vs mod), and Merge
            (vanilla vs mod).
          </p>
          <div class="flex flex-col gap-4">
            <div class="flex flex-col gap-2">
              <label class="text-sm font-medium">Crusader Kings III (CK3)</label>
              <div class="flex gap-2">
                <InputText :model-value="gameInstallPathCk3" readonly class="flex-1"
                  placeholder="Select CK3 install directory" />
                <Button label="Browse" icon="pi pi-folder-open" @click="browseCk3" />
              </div>
            </div>
            <div class="flex flex-col gap-2">
              <label class="text-sm font-medium">Europa Universalis V (EU5)</label>
              <div class="flex gap-2">
                <InputText :model-value="gameInstallPathEu5" readonly class="flex-1"
                  placeholder="Select EU5 install directory" />
                <Button label="Browse" icon="pi pi-folder-open" @click="browseEu5" />
              </div>
            </div>
          </div>
        </template>
      </Card>
    </div>
  </div>
</template>

<script setup>
import { useAppSettings } from '../composables/useAppSettings'
import { SelectDirectory } from '../../bindings/paradox-modding-tool/fileservice.js'

const {
  game,
  gameInstallPathCk3,
  gameInstallPathEu5,
  setGame,
  setGameInstallPathCk3,
  setGameInstallPathEu5
} = useAppSettings()

const gameOptions = [
  { label: 'CK3', value: 'ck3' },
  { label: 'EU5', value: 'eu5' }
]

async function browseCk3() {
  try {
    const path = await SelectDirectory('Select Crusader Kings III install directory')
    if (path) await setGameInstallPathCk3(path)
  } catch (e) {
    alert('Error selecting directory: ' + (e?.message ?? e))
  }
}

async function browseEu5() {
  try {
    const path = await SelectDirectory('Select Europa Universalis V install directory')
    if (path) await setGameInstallPathEu5(path)
  } catch (e) {
    alert('Error selecting directory: ' + (e?.message ?? e))
  }
}
</script>
