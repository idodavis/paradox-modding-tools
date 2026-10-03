<script setup lang="ts">
/**
 * Draft card to register a game install. Selecting a detected Steam path
 * only fills the form; Add install persists it.
 */
import { computed, shallowRef, watch } from "vue";
import FileSelector from "./FileSelector.vue";
import { DetectGameVersion } from "@services/workspaceservice";
import type { DetectedInstall } from "@services/internal/game/models";

const path = defineModel<string>("path", { required: true });
const name = defineModel<string>("name", { required: true });

const props = withDefaults(
  defineProps<{
    gameId: string;
    detected?: DetectedInstall[] | null;
    savedPaths?: string[];
    loading?: boolean;
  }>(),
  { detected: () => [], savedPaths: () => [], loading: false },
);

const emit = defineEmits<{
  add: [];
}>();

const detectedVersion = shallowRef("");

/** Lowercased slash-normalized path for draft vs saved matching. */
function pathKey(p: string): string {
  return p.replace(/\\/g, "/").toLowerCase();
}

const availableDetected = computed(() => {
  const saved = new Set((props.savedPaths ?? []).map(pathKey));
  return (props.detected ?? []).filter(
    (d) => d.path && !saved.has(pathKey(d.path)),
  );
});

const canAdd = computed(() => !!path.value && !!name.value.trim());
const isDraft = computed(() => !!path.value);

/** Prefill the draft from a Steam-detected folder. Does not persist. */
function selectDetected(d: DetectedInstall): void {
  path.value = d.path;
  detectedVersion.value = d.version || "";
  if (!name.value) {
    name.value = d.version ? `Steam ${d.version}` : "Steam";
  }
}

watch(path, async (p) => {
  detectedVersion.value = p ? await DetectGameVersion(props.gameId, p) : "";
});
</script>

<template>
  <UCard
    variant="subtle"
    title="Add an install"
    description="Draft — not saved until you click Add install."
  >
    <div class="space-y-4">
      <UBadge
        v-if="isDraft"
        label="Draft — not added yet"
        color="warning"
        variant="subtle"
        size="sm"
      />
      <div v-if="availableDetected.length" class="space-y-2">
        <p class="text-sm font-medium">Found on this machine</p>
        <p class="text-xs text-muted">
          Select a path to fill the form. You still have to add the install.
        </p>
        <div
          v-for="d in availableDetected"
          :key="d.path"
          class="flex items-center justify-between gap-2 rounded border p-2"
          :class="pathKey(d.path) === pathKey(path)
            ? 'border-primary bg-primary/10'
            : 'border-default'"
        >
          <span class="min-w-0 text-sm">
            <span class="block truncate">{{ d.name || gameId }} at {{ d.path }}</span>
            <span v-if="d.version" class="text-xs text-muted">{{ d.version }}</span>
          </span>
          <UButton
            label="Select"
            size="xs"
            variant="outline"
            @click="selectDetected(d)"
          />
        </div>
      </div>
      <UFormField
        label="Install path"
        description="Top-level game folder. Needed to scan vanilla and power most features."
      >
        <FileSelector
          v-model="path"
          mode="folder"
          dialog-title="Select game install folder"
          placeholder="C:\Program Files (x86)\Steam\steamapps\common\GAME_NAME"
        />
      </UFormField>
      <UFormField label="Install name">
        <UInput v-model="name" placeholder="e.g. Steam 1.14.0" />
      </UFormField>
      <p v-if="detectedVersion" class="text-xs text-muted">
        detected: {{ detectedVersion }} from launcher/launcher-settings.json
      </p>
      <UButton
        label="Add install"
        icon="i-lucide-plus"
        size="sm"
        :disabled="!canAdd"
        :loading="loading"
        @click="emit('add')"
      />
    </div>
  </UCard>
</template>
