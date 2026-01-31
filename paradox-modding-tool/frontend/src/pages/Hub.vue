<template>
  <div class="p-4">
    <div class="flex gap-6 max-w-8xl mx-auto w-full pt-6 h-full min-h-0">
      <!-- Main: tool cards -->
      <div class="flex-1 min-w-0 flex flex-col">
        <div class="grid gap-4 grid-cols-1 sm:grid-cols-2">
          <Card
            class="cursor-pointer transition-all hover:shadow-lg focus-within:ring-2 focus-within:ring-(--p-primary-500)"
            @click="emit('select', 'modding-docs')">
            <template #content>
              <div class="flex flex-col gap-2">
                <span class="text-lg font-semibold">Modding Docs</span>
                <span class="text-sm text-(--p-surface-400)">Browse script docs (.info /
                  readme.txt) and modding wikis for CK3 and EU5.</span>
                <Button label="Open" icon="pi pi-arrow-right" icon-pos="right" variant="outlined" class="mt-2 w-fit"
                  @click.stop="emit('select', 'modding-docs')" />
              </div>
            </template>
          </Card>
          <Card
            class="cursor-pointer transition-all hover:shadow-lg focus-within:ring-2 focus-within:ring-(--p-primary-500)"
            @click="emit('select', 'comparison')">
            <template #content>
              <div class="flex flex-col gap-2">
                <span class="text-lg font-semibold">File Compare</span>
                <span class="text-sm text-(--p-surface-400)">Compare two file sets or
                  directories and view diffs side-by-side or unified.</span>
                <Button label="Open" icon="pi pi-arrow-right" icon-pos="right" variant="outlined" class="mt-2 w-fit"
                  @click.stop="emit('select', 'comparison')" />
              </div>
            </template>
          </Card>
          <Card
            class="cursor-pointer transition-all hover:shadow-lg focus-within:ring-2 focus-within:ring-(--p-primary-500)"
            @click="emit('select', 'merge')">
            <template #content>
              <div class="flex flex-col gap-2">
                <span class="text-lg font-semibold">Script merger</span>
                <span class="text-sm text-(--p-surface-400)">Merge Paradox script files
                  (base + mod) with configurable options.</span>
                <Button label="Open" icon="pi pi-arrow-right" icon-pos="right" variant="outlined" class="mt-2 w-fit"
                  @click.stop="emit('select', 'merge')" />
              </div>
            </template>
          </Card>
          <Card
            class="cursor-pointer transition-all hover:shadow-lg focus-within:ring-2 focus-within:ring-(--p-primary-500)"
            @click="emit('select', 'inventory')">
            <template #content>
              <div class="flex flex-col gap-2">
                <span class="text-lg font-semibold">Object Inventory</span>
                <span class="text-sm text-(--p-surface-400)">Extract and explore game
                  objects from script files, view references and dependencies.</span>
                <Button label="Open" icon="pi pi-arrow-right" icon-pos="right" variant="outlined" class="mt-2 w-fit"
                  @click.stop="emit('select', 'inventory')" />
              </div>
            </template>
          </Card>
        </div>
      </div>

      <!-- Side: News banner -->
      <aside class="flex-1 min-w-100 max-w-150">
        <Card class="flex flex-col h-full bg-(--p-surface-100) border-(--p-surface-700)">
          <template #content>
            <div class="flex flex-col gap-4 py-2">
              <div class="flex items-center gap-2">
                <i class="pi pi-megaphone text-xl" />
                <span class="text-xl font-semibold uppercase">Latest Patch Notes</span>
              </div>
              <p v-if="patchNotesLoading" class="text-sm text-(--p-surface-400)">Loading…</p>
              <template v-else-if="latestPatchNotes">
                <h1 v-if="latestPatchNotes.title" class="text-lg text-(--p-surface-400)">
                  {{ latestPatchNotes.title }}
                </h1>
                <p v-if="latestPatchNotes.description" class="text-(--p-surface-400)">
                  {{ latestPatchNotes.description }}
                </p>
              </template>
              <Button label="Open patch notes" icon="pi pi-external-link" class="w-full mt-auto"
                :disabled="!latestPatchNotes?.url" @click="() => openURLInBrowser(latestPatchNotes?.url)" />
            </div>
          </template>
        </Card>
      </aside>
    </div>
  </div>
</template>

<script setup>
import { ref, watch, onMounted } from 'vue'
import Button from 'primevue/button'
import Card from 'primevue/card'
import { GetLatestPatchNotes } from '../../bindings/paradox-modding-tool/settingsservice.js'
import { useAppSettings } from '../composables/useAppSettings'
import { openURLInBrowser } from '../utils/general'

const emit = defineEmits(['select'])
const { game } = useAppSettings()

const latestPatchNotes = ref(null)
const patchNotesLoading = ref(false)

async function loadLatestPatchNotes() {
  patchNotesLoading.value = true
  latestPatchNotes.value = null
  try {
    const result = await GetLatestPatchNotes(game.value)
    latestPatchNotes.value = result
  } catch {
    latestPatchNotes.value = null
  } finally {
    patchNotesLoading.value = false
  }
}


onMounted(loadLatestPatchNotes)
watch(game, loadLatestPatchNotes)
</script>
