<!--
  This shows a listing's pictures and videos, with a line about its thumbnail under them. Adding or removing one
  changes the mod's files at once.
-->
<script setup lang="ts">
import { computed, shallowRef } from "vue";
import { useMutation, useQuery } from "@pinia/colada";

import { bytes, describeCount, describePicture, plural } from "../../lib/format";
import { fileUrl, headerQuery } from "../../queries";
import { useWorkspaceStore } from "../../stores/workspace";
import FileSelector, { IMAGES } from "../../ui/FileSelector.vue";
import * as steam from "@services/steam/service";
import type { Comparison, Listing, Preview } from "@services/steam/models";

/** COLUMNS is the number of places in a gallery row. */
const COLUMNS = 6;
/** STEAM_LIMIT is the size in bytes from which Steam turns a picture down. */
const STEAM_LIMIT = 1048576;
const OVER_LIMIT = "Over Steam's 1 MB limit.";
const RESIZED = "Resized for Steam when you upload. Your file stays as it is.";
const RESIZE_GIF = "Resize the GIF: Steam takes pictures under 1 MB.";

const {
  modId,
  listingKey,
  listing,
  comparison = undefined,
} = defineProps<{
  modId: string;
  listingKey: string;
  listing: Listing;
  comparison?: Comparison;
}>();

const workspace = useWorkspaceStore();
const link = shallowRef("");
const linking = shallowRef(false);
// The picture or video shown large. It stays set while the dialog closes.
const viewing = shallowRef<{ preview: Preview; place: number }>();
// Whether the large view is open.
const viewed = shallowRef(false);

const previews = computed(() => listing.previews ?? []);
// An oversized picture takes two places with its warning beside it. At a row's end it takes one, with a tooltip.
const tiles = computed(() => {
  let column = 0;
  return previews.value.map((preview) => {
    const warning = describeSize(preview.path, preview.bytes);
    const wide = !!warning && column < COLUMNS - 1;
    column = (column + (wide ? 2 : 1)) % COLUMNS;
    return { preview, warning, wide };
  });
});
// How many pictures Steam has that the gallery lacks.
const missing = computed(
  () => comparison?.page?.find((difference) => difference.field === "previews")?.removed?.length ?? 0,
);
const { data: header } = useQuery(() => ({
  ...headerQuery(listing.thumbnail),
  enabled: workspace.isOpen() && !!listing.thumbnail,
}));
const hint = computed(() => {
  const count = previews.value.length
    ? `${plural(previews.value.length, "picture or video", "pictures and videos")}, shown on Steam only.`
    : "No pictures or videos yet.";
  if (!listing.thumbnail) return `${count} No thumbnail yet.`;
  const size = header.value ? `, ${describePicture(header.value)},` : "";
  return `${count} The thumbnail${size} shows in the launcher and on Steam.`;
});
const thumbnailWarning = computed(() => (header.value ? describeSize(listing.thumbnail, header.value.bytes) : ""));
const viewTitle = computed(() => {
  if (!viewing.value) return "";
  const { preview, place } = viewing.value;
  return `${preview.type === "video" ? "Video" : "Picture"} ${place} of ${previews.value.length}`;
});

// Each of these sends release:changed, which reloads the listing.
const { mutate: add } = useMutation({ mutation: (preview: Preview) => steam.AddPreview(modId, listingKey, preview) });
const { mutate: remove } = useMutation({
  mutation: (preview: Preview) => steam.RemovePreview(modId, listingKey, preview),
});
const { mutate: importPreviews, isLoading: importing } = useMutation({
  mutation: () => steam.FetchPreviews(modId, listingKey),
});

/** addImage adds a picture file to the end of the gallery. */
function addImage(path: string): void {
  add({ path, type: "image", url: "", video: "", bytes: 0 });
}

/** addVideo adds the typed YouTube link to the end of the gallery. A blank link adds nothing. */
function addVideo(): void {
  const url = link.value.trim();
  link.value = "";
  linking.value = false;
  if (url) add({ path: "", type: "video", url, video: "", bytes: 0 });
}

/** describeSize returns what Steam does to a picture that is too big, or "" if Steam takes it as it is. */
function describeSize(path: string, size: number): string {
  if (size < STEAM_LIMIT) return "";
  return path.toLowerCase().endsWith(".gif") ? RESIZE_GIF : RESIZED;
}

/** view shows a picture or video large, with its place in the gallery counted from one. */
function view(preview: Preview, place: number): void {
  viewing.value = { preview, place };
  viewed.value = true;
}

/** drawPreview returns the URL of a preview's picture. A video's is its YouTube thumbnail. */
function drawPreview(preview: Preview): string {
  return preview.type === "video" ? `https://img.youtube.com/vi/${preview.video}/mqdefault.jpg` : fileUrl(preview.path);
}
</script>

<template>
  <div class="flex flex-col gap-2.5">
    <div class="grid grid-cols-6 gap-2">
      <div
        v-for="({ preview, warning, wide }, index) in tiles"
        :key="preview.path || preview.url"
        class="flex gap-2"
        :class="{ 'col-span-2': wide }"
      >
        <div
          class="group relative h-16.5 min-w-0 flex-1 overflow-hidden rounded-md border bg-(--bg-0)"
          :class="warning ? 'border-warning ring-1 ring-warning' : 'border-accented'"
          :title="warning && !wide ? `${OVER_LIMIT} ${warning}` : undefined"
        >
          <button
            type="button"
            class="size-full"
            :aria-label="`Open ${preview.path || preview.url}`"
            @click="view(preview, index + 1)"
          >
            <img :src="drawPreview(preview)" alt="" class="size-full object-cover" />
          </button>
          <span
            v-if="preview.type === 'video'"
            class="pointer-events-none absolute inset-0 m-auto flex size-7 items-center justify-center rounded-full bg-(--bg-0)/70"
          >
            <UIcon name="i-lucide-play" class="size-3.5 text-highlighted" />
          </span>
          <UBadge
            v-if="warning"
            :label="bytes(preview.bytes)"
            color="warning"
            variant="solid"
            size="sm"
            class="pointer-events-none absolute start-1 bottom-1"
          />
          <UButton
            label="Remove"
            color="error"
            variant="solid"
            size="xs"
            class="absolute end-1 top-1 opacity-0 group-focus-within:opacity-100 group-hover:opacity-100"
            @click="remove(preview)"
          />
        </div>
        <div v-if="wide" class="flex w-36 shrink-0 flex-col gap-0.5 text-11 leading-4">
          <span class="font-medium text-warning">{{ OVER_LIMIT }}</span>
          <span class="text-dimmed">{{ warning }}</span>
        </div>
      </div>
      <FileSelector path="" :filter="IMAGES" @update:path="addImage">
        <template #default="{ browse }">
          <button
            type="button"
            class="flex h-16.5 w-full flex-col items-center justify-center gap-1 rounded-md border border-dashed border-accented text-11 text-dimmed hover:text-toned"
            @click="browse"
          >
            <UIcon name="i-lucide-plus" class="size-3.5" />
            Add image
          </button>
        </template>
      </FileSelector>
    </div>

    <p v-if="thumbnailWarning" class="text-12 text-warning">
      The thumbnail is {{ header ? bytes(header.bytes) : "" }}, over Steam's 1 MB limit. {{ thumbnailWarning }}
    </p>

    <div class="flex items-center gap-1.5">
      <span class="min-w-0 grow text-11 text-dimmed">{{ hint }}</span>
      <UButton
        v-if="missing"
        :label="`Import ${describeCount(missing)} from Steam`"
        icon="i-lucide-download"
        size="sm"
        variant="ghost"
        class="shrink-0"
        :loading="importing"
        @click="importPreviews()"
      />
      <UPopover v-model:open="linking">
        <UButton label="Add a YouTube link" icon="i-lucide-play" size="sm" variant="ghost" class="shrink-0" />
        <template #content>
          <form class="flex gap-2 p-2.5" @submit.prevent="addVideo">
            <UInput v-model="link" placeholder="A YouTube link" autofocus class="w-64" />
            <UButton type="submit" label="Add" color="primary" variant="solid" :disabled="!link.trim()" />
          </form>
        </template>
      </UPopover>
    </div>

    <UModal
      v-model:open="viewed"
      :title="viewTitle"
      :description="viewing?.preview.path || viewing?.preview.url"
      :ui="{ content: 'sm:max-w-4xl', description: 'break-all' }"
    >
      <template #body>
        <div v-if="viewing" class="aspect-video w-full overflow-hidden rounded-lg border border-default bg-(--bg-0)">
          <iframe
            v-if="viewing.preview.type === 'video'"
            :src="`https://www.youtube.com/embed/${encodeURIComponent(viewing.preview.video)}`"
            sandbox="allow-scripts allow-same-origin allow-presentation"
            title="The YouTube video"
            allow="encrypted-media; picture-in-picture; fullscreen"
            referrerpolicy="strict-origin-when-cross-origin"
            class="size-full"
          />
          <img v-else :src="fileUrl(viewing.preview.path)" alt="" class="size-full object-contain" />
        </div>
      </template>
    </UModal>
  </div>
</template>
