<script setup lang="ts">
/**
 * Create a new mod folder (descriptor, loc stub, common/, events/, readmes).
 */
import { computed, ref, watch } from "vue";
import FileSelector from "./FileSelector.vue";
import { CreateMod, DefaultModParent } from "@services/workspaceservice";
import { ReadFileBase64 } from "@services/fileservice";
import { LOC_LANG_ITEMS } from "../stores/workspace";

const props = withDefaults(
  defineProps<{
    gameId: string;
    locLang?: string;
    supportedVersion?: string;
  }>(),
  { locLang: "english", supportedVersion: "" },
);

const emit = defineEmits<{
  created: [path: string, thumbnail: string];
}>();

const toast = useToast();
const name = ref("");
const description = ref("");
const parent = ref("");
const lang = ref(props.locLang);
const thumbnailSrc = ref("");
const thumbPreview = ref("");
const creating = ref(false);

watch(
  () => props.gameId,
  async (id) => {
    parent.value = (await DefaultModParent(id)) ?? "";
  },
  { immediate: true },
);

watch(
  () => props.locLang,
  (v) => {
    if (v) lang.value = v;
  },
);

watch(thumbnailSrc, async (src) => {
  thumbPreview.value = "";
  if (!src) return;
  try {
    const file = await ReadFileBase64(src);
    if (!file?.exists || !file.b64) return;
    const ext = src.split(".").pop()?.toLowerCase() ?? "";
    const mime = ext === "svg" ? "image/svg+xml"
      : ext === "jpg" || ext === "jpeg" ? "image/jpeg"
        : "image/png";
    thumbPreview.value = `data:${mime};base64,${file.b64}`;
  } catch {
    /* skip unreadable thumbs */
  }
});

/**
 * Where the mod will be created — the folder name as Go's ModSlug makes it, not
 * as typed. It lowercases, turns every run of non-alphanumerics into one
 * underscore, trims those from the ends, and falls back to "mod".
 *
 * This is a second copy of a Go rule and it is kept deliberately: previewing
 * `… / My Great Mod!` for something created at `…\my_great_mod` is a promise the
 * app does not keep, and ModSlug is not bound. Bind it and delete this.
 */
function modSlug(name: string): string {
  const out = name
    .trim()
    .toLowerCase()
    // \p{Nd}, not \p{N}: Go's ModSlug uses unicode.IsDigit, which is Nd alone.
    .replace(/[^\p{L}\p{Nd}]+/gu, "_")
    .replace(/^_+|_+$/g, "");
  return out || "mod";
}

const preview = computed(() => {
  const folder = name.value.trim();
  if (!parent.value || !folder) return "";
  return `${parent.value.replace(/[\\/]+$/, "")} / ${modSlug(folder)}`;
});

/** Write the skeleton and emit the new mod root. */
async function submit(): Promise<void> {
  const label = name.value.trim();
  if (!label || !parent.value) return;
  creating.value = true;
  try {
    const path = await CreateMod(
      props.gameId,
      parent.value,
      label,
      lang.value,
      props.supportedVersion ?? "",
      description.value,
      thumbnailSrc.value,
    );
    if (!path) throw new Error("Failed to create mod");
    // Where the thumbnail landed is Go's answer, not one to recompute here:
    // it is per-game (EU5 keeps it in .metadata) and GameInfo.ThumbnailDir is
    // backend-only. AddWorkspaceMod discovers it when this is empty.
    emit("created", path, "");
    name.value = "";
    description.value = "";
    thumbnailSrc.value = "";
  } catch (e) {
    toast.add({
      title: e instanceof Error ? e.message : String(e),
      color: "error",
    });
  } finally {
    creating.value = false;
  }
}
</script>

<template>
  <div class="space-y-3">
    <p class="text-sm text-muted">
      Creates a descriptor, empty common/ and events/ folders, readmes, and a
      localization stub (UTF-8 BOM). Description is not written into the descriptor.
    </p>
    <UFormField label="Mod name" required>
      <UInput v-model="name" placeholder="My Mod" />
    </UFormField>
    <UFormField label="Description" hint="Optional; defaults to the mod name.">
      <UTextarea
        v-model="description"
        placeholder="optional; defaults to name"
        :rows="3"
        autoresize
        :maxrows="6"
      />
    </UFormField>
    <UFormField label="Parent folder">
      <FileSelector
        v-model="parent"
        mode="folder"
        dialog-title="Select folder for the new mod"
      />
    </UFormField>
    <p v-if="preview" class="text-xs text-muted">
      Creates {{ preview }} (folder name is made filesystem-safe).
    </p>
    <UFormField label="Loc language">
      <USelect v-model="lang" :items="LOC_LANG_ITEMS" value-key="value" />
    </UFormField>
    <UFormField label="Thumbnail">
      <div class="flex items-center gap-2">
        <FileSelector
          v-model="thumbnailSrc"
          mode="file"
          dialog-title="Select thumbnail"
          file-filter="*.png; *.jpg; *.jpeg; *.svg"
          class="min-w-0 flex-1"
        />
        <UButton
          v-if="thumbnailSrc"
          label="Clear"
          size="xs"
          color="neutral"
          variant="ghost"
          @click="thumbnailSrc = ''"
        />
      </div>
    </UFormField>
    <img
      v-if="thumbPreview"
      :src="thumbPreview"
      alt=""
      class="size-6 rounded-sm object-cover"
    />
    <UButton
      label="Create mod"
      icon="i-lucide-plus"
      size="sm"
      :disabled="!name.trim() || !parent"
      :loading="creating"
      @click="submit()"
    />
  </div>
</template>
