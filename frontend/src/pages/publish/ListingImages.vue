<script setup lang="ts">
/**
 * The listing's images (the PublishListing board's Images, laid out as O-R38 sets them): the thumbnail with
 * Change, then the extra screenshots and YouTube videos as Steam's gallery, the picked one large (a video plays
 * in place) with a strip of 116 by 65 pictures, Steam's size, beside it or under it. Each change saves at once.
 */
import { computed, shallowRef } from "vue";
import { useMutation, useQuery, useQueryCache } from "@pinia/colada";
import type { Listing, WorkspaceMod } from "@services/models";
import type { Preview } from "@services/internal/steam/models";
import {
  AddWorkshopPreview,
  AddWorkshopVideo,
  ImportSteamExtras,
  RemoveWorkshopPreview,
  RemoveWorkshopVideo,
} from "@services/releaseservice";
import { useListingImage } from "../../composables/useListingImage";
import { imageSize } from "../../lib/format";
import { pickFile } from "../../lib/nativeDialog";
import { imageQuery, releaseKeys } from "../../queries";
import { modColor } from "../../stores/workspace";
import Thumb from "../../ui/Thumb.vue";

const { listing, mod } = defineProps<{ listing: Listing; mod: WorkspaceMod }>();

const queryCache = useQueryCache();
const { change, changing } = useListingImage(() => listing.workspaceId);
const { data: thumbnail } = useQuery(() => imageQuery(listing.thumbnailAbs));

const extras = computed(() => listing.previews ?? []);
const picked = shallowRef(0);
const shown = computed(() => extras.value[Math.min(picked.value, extras.value.length - 1)]);
const { data: image } = useQuery(() => imageQuery(shown.value?.kind === "image" ? shown.value.abs : ""));
/** A video's picture, as YouTube serves it. */
const still = (id: string) => `https://img.youtube.com/vi/${id}/mqdefault.jpg`;

const { mutate: edit, isLoading: editing } = useMutation({
  mutation: (save: (workspaceId: string, modId: string) => Promise<unknown>) =>
    save(listing.workspaceId, listing.modId),
  onSettled: () => queryCache.invalidateQueries({ key: releaseKeys.mod(listing.workspaceId, listing.modId) }),
});
async function addImage(): Promise<void> {
  const path = await pickFile("A screenshot", "*.png; *.jpg; *.jpeg; *.gif; *.webp");
  if (path) edit((workspaceId, modId) => AddWorkshopPreview(workspaceId, modId, path));
}
const link = shallowRef("");
function addVideo(): void {
  const url = link.value.trim();
  link.value = "";
  if (url) edit((workspaceId, modId) => AddWorkshopVideo(workspaceId, modId, url));
}
function remove(extra: Preview): void {
  edit((workspaceId, modId) =>
    extra.kind === "youtube"
      ? RemoveWorkshopVideo(workspaceId, modId, extra.id)
      : RemoveWorkshopPreview(workspaceId, modId, extra.rel),
  );
}
</script>

<template>
  <!-- the thumbnail on its own row at its own shape; from 672px wide the strip runs down the preview's side and
       the preview takes the height left, so the column scrolls only below 180px of it (O-R38) -->
  <div class="@container flex grow flex-col gap-3.5 py-4.5">
    <div class="flex items-center gap-4">
      <img
        v-if="thumbnail"
        :src="thumbnail.src"
        alt=""
        class="h-36 max-w-72 shrink-0 rounded-lg border border-accented bg-(--bg-0) object-contain"
      />
      <Thumb v-else :color="modColor(mod)" size="2xl" />
      <div class="flex min-w-0 flex-col items-start gap-1">
        <span class="text-12 font-medium">Thumbnail</span>
        <span class="text-11 text-dimmed">Shown in the launcher and on Steam</span>
        <span class="max-w-full truncate font-mono text-12 text-toned" :title="listing.thumbnailRel">{{
          listing.thumbnailRel || "None yet"
        }}</span>
        <span v-if="thumbnail" class="text-11 text-dimmed">{{ imageSize(thumbnail) }}</span>
        <UButton label="Change" :loading="changing" class="mt-1" @click="change(listing.modId)" />
      </div>
    </div>

    <div class="flex max-w-240 min-w-0 flex-col gap-2 @2xl:grow">
      <div class="flex items-center gap-2">
        <span class="text-12 font-medium">Screenshots and videos</span>
        <span class="text-11 text-dimmed">{{ extras.length }} of 10, on Steam only</span>
      </div>
      <div v-if="shown" class="flex flex-col gap-2 @2xl:min-h-45 @2xl:grow @2xl:basis-0 @2xl:flex-row">
        <div
          class="aspect-video w-full overflow-hidden rounded-lg border border-default bg-(--bg-0) @2xl:h-full @2xl:w-auto @2xl:min-w-0 @2xl:flex-initial"
        >
          <iframe
            v-if="shown.kind === 'youtube'"
            :src="`https://www.youtube.com/embed/${shown.id}`"
            title="The YouTube video"
            allow="encrypted-media; picture-in-picture; fullscreen"
            referrerpolicy="strict-origin-when-cross-origin"
            class="size-full"
          />
          <img v-else-if="image" :src="image.src" alt="" class="size-full object-contain" />
        </div>
        <!-- beside the preview the strip is as tall as it and scrolls -->
        <div class="relative @2xl:w-31 @2xl:shrink-0">
          <div class="flex gap-2 overflow-auto pb-1 @2xl:absolute @2xl:inset-0 @2xl:flex-col @2xl:pb-0">
            <button
              v-for="(extra, i) in extras"
              :key="extra.rel || extra.id"
              type="button"
              class="relative m-0.5 flex h-16.25 w-29 shrink-0 rounded-md"
              :class="extra === shown ? 'ring-2 ring-primary' : 'opacity-60 hover:opacity-100'"
              :aria-label="extra.kind === 'youtube' ? `YouTube video ${extra.id}` : extra.rel"
              @click="picked = i"
            >
              <Thumb v-if="extra.kind === 'image'" :path="extra.abs" size="fill" />
              <template v-else>
                <img :src="still(extra.id)" alt="" class="size-full rounded-md border border-accented object-cover" />
                <span
                  class="absolute inset-0 m-auto flex size-7 items-center justify-center rounded-full bg-(--bg-0)/70"
                >
                  <UIcon name="i-lucide-play" class="size-3.5 text-highlighted" />
                </span>
              </template>
            </button>
          </div>
        </div>
      </div>
      <p v-else class="text-12 text-dimmed">
        None yet. An upload leaves the ones already on Steam as they are; Import from Steam brings them here.
      </p>
      <div class="flex flex-wrap items-center gap-2">
        <template v-if="shown">
          <span class="min-w-0 truncate font-mono text-12 text-toned">
            {{ shown.kind === "youtube" ? `youtube.com/watch?v=${shown.id}` : shown.rel }}
          </span>
          <UButton
            label="Remove"
            icon="i-lucide-trash-2"
            size="sm"
            color="error"
            variant="ghost"
            @click="remove(shown)"
          />
          <span class="grow" />
        </template>
        <UButton label="Add image" icon="i-lucide-plus" size="sm" @click="addImage" />
        <UButton
          v-if="listing.remoteFileId"
          label="Import from Steam"
          icon="i-lucide-download"
          size="sm"
          variant="ghost"
          :loading="editing"
          @click="edit(ImportSteamExtras)"
        />
        <UPopover>
          <UButton label="Add a YouTube link" icon="i-lucide-plus" size="sm" variant="ghost" />
          <template #content>
            <form class="flex gap-2 p-2.5" @submit.prevent="addVideo">
              <UInput v-model="link" placeholder="A YouTube link or video id" autofocus class="w-64" />
              <UButton type="submit" label="Add" color="primary" variant="solid" />
            </form>
          </template>
        </UPopover>
      </div>
    </div>
  </div>
</template>
