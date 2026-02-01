<template>
  <div class="relative min-h-full">
    <!-- Background: image layer (faded) -->
    <div class="absolute inset-0 bg-cover bg-center opacity-30"
      :style="{ backgroundImage: `url(${backgroundImage})` }" />
    <!-- Radial vignette: center much more faded than edges -->
    <div class="absolute inset-0 bg-[radial-gradient(ellipse_at_center,var(--p-surface-900)_0%,transparent_65%)]" />
    <!-- Content above background -->
    <div class="relative z-10 p-4 h-full flex flex-col">
      <div class="flex gap-6 max-w-8xl mx-auto w-full min-h-0 flex-1 items-stretch">
        <!-- Main: tool cards (centered vertically in column) -->
        <div class="flex-1 min-w-0 flex flex-col justify-center">
          <div class="grid gap-4 grid-cols-1 sm:grid-cols-2">
            <Card
              class="cursor-pointer transition-all hover:shadow-xl! focus-within:ring-2 focus-within:ring-(--p-primary-500) bg-[color-mix(in_srgb,var(--p-surface-800)_88%,transparent)]! shadow-[0_10px_15px_-3px_rgb(0_0_0/0.2),0_4px_6px_-4px_rgb(0_0_0/0.15)]!"
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
              class="cursor-pointer transition-all hover:shadow-xl! focus-within:ring-2 focus-within:ring-(--p-primary-500) bg-[color-mix(in_srgb,var(--p-surface-800)_88%,transparent)]! shadow-[0_10px_15px_-3px_rgb(0_0_0/0.2),0_4px_6px_-4px_rgb(0_0_0/0.15)]!"
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
              class="cursor-pointer transition-all hover:shadow-xl! focus-within:ring-2 focus-within:ring-(--p-primary-500) bg-[color-mix(in_srgb,var(--p-surface-800)_88%,transparent)]! shadow-[0_10px_15px_-3px_rgb(0_0_0/0.2),0_4px_6px_-4px_rgb(0_0_0/0.15)]!"
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
              class="cursor-pointer transition-all hover:shadow-xl! focus-within:ring-2 focus-within:ring-(--p-primary-500) bg-[color-mix(in_srgb,var(--p-surface-800)_88%,transparent)]! shadow-[0_10px_15px_-3px_rgb(0_0_0/0.2),0_4px_6px_-4px_rgb(0_0_0/0.15)]!"
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

        <!-- Side: Latest Patch Notes – medieval-style vertical banner (taller, centered in column) -->
        <aside class="flex-1 min-w-50 max-w-95 flex flex-col justify-center items-center py-4 px-2">
          <div class="relative flex flex-col min-h-128 w-full max-w-100">
            <!-- Shadow layer behind banner (same shape, offset + blur; stays in flow so not clipped) -->
            <div
              class="absolute inset-0 [clip-path:polygon(0_0,100%_0,100%_100%,50%_calc(100%-56px),0_100%)] translate-x-2 translate-y-2 bg-black/35 blur-[80px] pointer-events-none"
              aria-hidden="true" style="z-index: -1" />
            <!-- Banner content (clipped shape) -->
            <div
              class="flex flex-col flex-1 min-h-0 [clip-path:polygon(0_0,100%_0,100%_100%,50%_calc(100%-56px),0_100%)]">
              <!-- Banner image strip (game wallpaper) -->
              <div class="h-24 bg-cover bg-center rounded-t-lg border border-b-0 border-(--p-surface-800)"
                :style="{ backgroundImage: `url(${backgroundImage})` }" />
              <!-- Banner body with V-cut bottom -->
              <div
                class="flex flex-col flex-1 min-h-0 gap-4 p-4 border border-t-0 border-(--p-surface-800) bg-[color-mix(in_srgb,var(--p-surface-800)_88%,transparent)]">
                <div class="flex items-center gap-2 min-w-0">
                  <i class="pi pi-megaphone text-xl text-(--p-primary-400)" />
                  <span class="text-lg font-semibold uppercase text-(--p-surface-200) truncate">Latest Patch
                    Notes</span>
                </div>
                <p v-if="patchNotesLoading" class="text-sm text-(--p-surface-400)">Loading…</p>
                <template v-else-if="latestPatchNotes">
                  <p v-if="latestPatchNotes.description" class="text-lg text-(--p-surface-300) line-clamp-4">
                    {{ latestPatchNotes.description }}
                  </p>
                  <p v-if="latestPatchNotes.title" class="text-base font-medium text-(--p-surface-400)">
                    {{ latestPatchNotes.title }}
                  </p>
                </template>
                <!-- Button centered, just above the V-cut (56px from bottom) -->
                <div class="mt-auto flex justify-center pb-14">
                  <Button label="Open" icon="pi pi-external-link" icon-pos="right" size="medium" severity="success"
                    rounded :disabled="!latestPatchNotes?.url" @click="() => openURLInBrowser(latestPatchNotes?.url)" />
                </div>
              </div>
            </div>
          </div>
        </aside>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted } from 'vue'
import { GetLatestPatchNotes } from '../../bindings/paradox-modding-tool/settingsservice.js'
import { useAppSettings } from '../composables/useAppSettings'
import { openURLInBrowser } from '../utils/general'
import ck3Bg from '../assets/CK3-All_Under_Heaven.jpg'
import eu5Bg from '../assets/EUV-Release.jpg'

const emit = defineEmits(['select'])
const { game } = useAppSettings()

const backgroundImage = computed(() => (game.value === 'eu5' ? eu5Bg : ck3Bg))

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
