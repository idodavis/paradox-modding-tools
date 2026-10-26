<!--
  This shows a listing's thumbnail, title, tags and requirements as players see them on its Workshop page, and edits
  each part where it shows.
-->
<script setup lang="ts">
import { computed, shallowRef } from "vue";
import { useQuery } from "@pinia/colada";

import { list, plural } from "../../lib/format";
import { dlcQuery, fileUrl, tagsQuery } from "../../queries";
import FileSelector, { IMAGES } from "../../ui/FileSelector.vue";
import type { Listing } from "@services/steam/models";

const listing = defineModel<Listing>("listing", { required: true });

// The thumbnail the browser could not show. Its tile says so instead.
const brokenThumbnail = shallowRef("");

const { data: tags } = useQuery(tagsQuery);
const { data: dlc } = useQuery(dlcQuery);

const categories = computed(() => [...new Set([...(tags.value?.categories ?? []), ...(listing.value.tags ?? [])])]);
const requirement = computed(() => {
  const mods = (listing.value.dependencies ?? []).map((one) => one.name);
  const count = listing.value.dlc?.length ?? 0;
  const needs = [...mods, ...(count ? [plural(count, "DLC", "DLC")] : [])];
  return needs.length ? `Requires ${list(needs)}` : "Nothing else needed";
});

/** update changes some fields of the listing. */
function update(changes: Partial<Listing>): void {
  listing.value = { ...listing.value, ...changes };
}

/** addTag adds a tag to the listing, unless it has it already. */
function addTag(tag: string): void {
  if (!listing.value.tags?.includes(tag)) update({ tags: [...(listing.value.tags ?? []), tag] });
}

/** toggleDLC adds a DLC to the listing's requirements when it is ticked, and removes it otherwise. */
function toggleDLC(id: string, required: boolean | "indeterminate"): void {
  const others = (listing.value.dlc ?? []).filter((one) => one !== id);
  update({ dlc: required === true ? [...others, id] : others });
}
</script>

<template>
  <div class="flex items-start gap-5">
    <FileSelector
      :path="listing.thumbnail"
      :filter="IMAGES"
      class="shrink-0"
      @update:path="update({ thumbnail: $event })"
    >
      <template #default="{ browse }">
        <button
          type="button"
          class="group relative size-33 shrink-0 overflow-hidden rounded-lg border border-accented bg-(--bg-0)"
          aria-label="Change the thumbnail"
          :title="listing.thumbnail"
          @click="browse"
        >
          <img
            v-if="listing.thumbnail && brokenThumbnail !== listing.thumbnail"
            :src="fileUrl(listing.thumbnail)"
            alt=""
            class="size-full object-cover"
            @error="brokenThumbnail = listing.thumbnail"
          />
          <span v-else class="flex size-full flex-col items-center justify-center gap-1 pb-5 text-11 text-dimmed">
            <UIcon :name="listing.thumbnail ? 'i-lucide-image-off' : 'i-lucide-image'" class="size-6" />
            {{ listing.thumbnail ? "File not found" : "No thumbnail" }}
          </span>
          <span class="absolute inset-x-0 bottom-0 bg-(--bg-0)/75 py-1 text-center text-11 text-toned">Change</span>
        </button>
      </template>
    </FileSelector>
    <div class="flex min-w-0 grow flex-col gap-2.5 pt-0.5">
      <UInput
        :model-value="listing.title"
        variant="none"
        size="xl"
        placeholder="The name players see"
        class="w-full"
        :ui="{ base: 'px-0 text-20' }"
        @update:model-value="update({ title: `${$event}` })"
      />
      <div class="flex flex-wrap items-center gap-1.5">
        <UBadge
          v-for="tag in listing.tags ?? []"
          :key="tag"
          :label="tag"
          color="neutral"
          variant="subtle"
          class="h-7 gap-1 rounded-full ps-2.5 pe-1.5 text-12"
        >
          <template #trailing>
            <UTooltip :text="`Remove ${tag}`">
              <UButton
                icon="i-lucide-x"
                size="xs"
                color="error"
                variant="link"
                class="p-0"
                :aria-label="`Remove ${tag}`"
                @click="update({ tags: (listing.tags ?? []).filter((one) => one !== tag) })"
              />
            </UTooltip>
          </template>
        </UBadge>
        <USelectMenu
          :model-value="listing.tags ?? []"
          :items="categories"
          multiple
          create-item
          :search-input="{ placeholder: 'Pick or type a tag' }"
          :content="{ align: 'start' }"
          size="sm"
          color="neutral"
          class="w-auto rounded-full pe-2.5"
          :ui="{ trailing: 'hidden', content: 'min-w-60' }"
          @update:model-value="update({ tags: $event })"
          @create="addTag"
        >
          <UIcon name="i-lucide-plus" class="size-3.5" />
          Tag
          <template #content-bottom>
            <span class="block px-2.5 py-1.5 text-11 text-dimmed">Type a new tag and press Enter to add it.</span>
          </template>
        </USelectMenu>
      </div>
      <div class="flex flex-wrap items-center gap-1.5">
        <UPopover :content="{ align: 'start' }">
          <UButton
            :label="requirement"
            icon="i-lucide-package"
            trailing-icon="i-lucide-chevron-down"
            size="sm"
            color="neutral"
            variant="subtle"
            class="rounded-full"
          />
          <template #content>
            <div class="flex max-h-96 w-96 flex-col gap-1 overflow-y-auto p-2.5">
              <span class="text-11 font-medium text-dimmed">Required mods</span>
              <p v-if="!listing.dependencies?.length" class="text-12 text-dimmed">The descriptor names none.</p>
              <UCheckbox
                v-for="one in listing.dependencies ?? []"
                :key="one.name"
                :model-value="!!one.workshopId"
                disabled
                :label="one.name"
                :description="one.workshopId ? 'From the descriptor' : 'Not on the Workshop, so not sent to Steam'"
                :ui="{ label: 'text-13 font-normal break-all', description: 'text-11' }"
              />
              <template v-if="dlc?.length">
                <USeparator class="my-1.5" />
                <span class="text-11 font-medium text-dimmed">Required DLC</span>
                <UCheckbox
                  v-for="one in dlc"
                  :key="one.id"
                  :model-value="listing.dlc?.includes(one.id) ?? false"
                  :label="one.name"
                  :ui="{ label: 'text-13 font-normal' }"
                  @update:model-value="toggleDLC(one.id, $event)"
                />
              </template>
            </div>
          </template>
        </UPopover>
      </div>
    </div>
  </div>
</template>
