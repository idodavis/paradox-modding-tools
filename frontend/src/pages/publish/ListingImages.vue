<!-- The thumbnail, the screenshots and the videos of a listing. Each change is saved at once. -->
<script setup lang="ts">
import { computed, shallowRef } from "vue";
import { useMutation, useQuery } from "@pinia/colada";

import { bytes } from "../../lib/format";
import { fileUrl, headerQuery } from "../../queries";
import FileSelector from "../../ui/FileSelector.vue";
import * as steam from "@services/steam/service";
import type { Listing, Preview } from "@services/steam/models";

const IMAGES = ["*.png", "*.jpg", "*.jpeg", "*.gif", "*.webp"];

const { modId, listing } = defineProps<{ modId: string; listing: Listing }>();

const selected = shallowRef(0);
const link = shallowRef("");

const previews = computed(() => listing.previews ?? []);
const current = computed(() => previews.value[Math.min(selected.value, previews.value.length - 1)]);
const { data: header } = useQuery(() => ({ ...headerQuery(listing.thumbnail), enabled: !!listing.thumbnail }));

const { mutate: write } = useMutation({ mutation: (next: Listing) => steam.WriteListing(modId, next) });
const { mutate: add } = useMutation({ mutation: (preview: Preview) => steam.AddPreview(modId, preview) });
const { mutate: remove } = useMutation({ mutation: (preview: Preview) => steam.RemovePreview(modId, preview) });
const { mutate: fetchPreviews, isLoading: fetching } = useMutation({ mutation: () => steam.FetchPreviews(modId) });

/** addVideo adds the typed link to the gallery as a video. A blank link adds nothing. */
function addVideo(): void {
  const url = link.value.trim();
  link.value = "";
  if (url) add({ path: "", type: "video", url, video: "", order: previews.value.length });
}
</script>

<template>
  <!-- the thumbnail on its own row at its own shape; from 672px wide the strip runs down the preview's side and
       the preview takes the height left, so the column scrolls only below 180px of it -->
  <div class="@container flex grow flex-col gap-3.5 py-4.5">
    <div class="flex items-center gap-4">
      <img
        v-if="listing.thumbnail"
        :src="fileUrl(listing.thumbnail)"
        alt=""
        class="h-36 max-w-72 shrink-0 rounded-lg border border-accented bg-(--bg-0) object-contain"
      />
      <div
        v-else
        class="grid h-36 w-64 shrink-0 place-items-center rounded-lg border border-dashed border-accented text-dimmed"
      >
        <UIcon name="i-lucide-image" class="size-6" />
      </div>
      <div class="flex min-w-0 flex-col items-start gap-1">
        <span class="text-12 font-medium">Thumbnail</span>
        <span class="text-11 text-dimmed"
          >Shown in the launcher and on Steam. A picture from elsewhere is copied in.</span
        >
        <span class="max-w-full truncate font-mono text-12 text-toned" :title="listing.thumbnail">{{
          listing.thumbnail || "None yet"
        }}</span>
        <span v-if="header" class="text-11 text-dimmed">
          {{ header.width }} by {{ header.height }}, {{ bytes(header.bytes) }}
        </span>
        <FileSelector
          :path="listing.thumbnail"
          :filter="IMAGES"
          class="mt-1"
          @update:path="write({ ...listing, thumbnail: $event })"
        >
          <template #default="{ browse }">
            <UButton label="Change" @click="browse" />
          </template>
        </FileSelector>
      </div>
    </div>

    <div class="flex max-w-240 min-w-0 flex-col gap-2 @2xl:grow">
      <div class="flex items-center gap-2">
        <span class="text-12 font-medium">Screenshots and videos</span>
        <span class="text-11 text-dimmed">{{ previews.length }} of 10, on Steam only</span>
      </div>
      <div v-if="current" class="flex flex-col gap-2 @2xl:min-h-45 @2xl:grow @2xl:basis-0 @2xl:flex-row">
        <div
          class="aspect-video w-full overflow-hidden rounded-lg border border-default bg-(--bg-0) @2xl:h-full @2xl:w-auto @2xl:min-w-0 @2xl:flex-initial"
        >
          <iframe
            v-if="current.type === 'video'"
            :src="`https://www.youtube.com/embed/${current.video}`"
            title="The YouTube video"
            allow="encrypted-media; picture-in-picture; fullscreen"
            referrerpolicy="strict-origin-when-cross-origin"
            class="size-full"
          />
          <img v-else :src="fileUrl(current.path)" alt="" class="size-full object-contain" />
        </div>
        <!-- beside the preview the strip is as tall as it and scrolls -->
        <div class="relative @2xl:w-31 @2xl:shrink-0">
          <div class="flex gap-2 overflow-auto pb-1 @2xl:absolute @2xl:inset-0 @2xl:flex-col @2xl:pb-0">
            <button
              v-for="(preview, index) in previews"
              :key="preview.path || preview.video"
              type="button"
              class="relative m-0.5 flex h-16.25 w-29 shrink-0 rounded-md"
              :class="preview === current ? 'ring-2 ring-primary' : 'opacity-60 hover:opacity-100'"
              :aria-label="preview.path || preview.url"
              @click="selected = index"
            >
              <img
                :src="
                  preview.type === 'video'
                    ? `https://img.youtube.com/vi/${preview.video}/mqdefault.jpg`
                    : fileUrl(preview.path)
                "
                alt=""
                class="size-full rounded-md border border-accented bg-(--bg-0) object-cover"
              />
              <span
                v-if="preview.type === 'video'"
                class="absolute inset-0 m-auto flex size-7 items-center justify-center rounded-full bg-(--bg-0)/70"
              >
                <UIcon name="i-lucide-play" class="size-3.5 text-highlighted" />
              </span>
            </button>
          </div>
        </div>
      </div>
      <p v-else class="text-12 text-dimmed">
        None yet. An upload leaves the ones already on Steam as they are; Import from Steam brings them here.
      </p>
      <div class="flex flex-wrap items-center gap-2">
        <template v-if="current">
          <span class="min-w-0 truncate font-mono text-12 text-toned">{{ current.path || current.url }}</span>
          <UButton
            label="Remove"
            icon="i-lucide-trash-2"
            size="sm"
            color="error"
            variant="ghost"
            @click="remove(current)"
          />
          <span class="grow" />
        </template>
        <FileSelector
          path=""
          :filter="IMAGES"
          @update:path="add({ path: $event, type: 'image', url: '', video: '', order: previews.length })"
        >
          <template #default="{ browse }">
            <UButton label="Add image" icon="i-lucide-plus" size="sm" @click="browse" />
          </template>
        </FileSelector>
        <UButton
          v-if="listing.workshopId"
          label="Import from Steam"
          icon="i-lucide-download"
          size="sm"
          variant="ghost"
          :loading="fetching"
          @click="fetchPreviews()"
        />
        <UPopover>
          <UButton label="Add a YouTube link" icon="i-lucide-plus" size="sm" variant="ghost" />
          <template #content>
            <form class="flex gap-2 p-2.5" @submit.prevent="addVideo">
              <UInput v-model="link" placeholder="A YouTube link" autofocus class="w-64" />
              <UButton type="submit" label="Add" color="primary" variant="solid" :disabled="!link.trim()" />
            </form>
          </template>
        </UPopover>
      </div>
    </div>
  </div>
</template>
