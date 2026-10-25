<!--
  The Publish page: the workspace's mods in a rail, and for the selected one the listing editors, the change note, the
  upload review and the compare with Steam.
-->
<script setup lang="ts">
import { computed, ref, shallowRef, watch } from "vue";
import { useMutation, useQuery, useQueryCache } from "@pinia/colada";
import { isEqual, mapValues, pick, pickBy } from "es-toolkit";
import type { StepperItem } from "@nuxt/ui";

import { list, plural, steamSentences, uploadSentences } from "../../lib/format";
import { comparisonQuery, keys, listingQuery, stateQuery, tagsQuery, uploadQuery } from "../../queries";
import { useWorkspaceStore } from "../../stores/workspace";
import ModAvatar from "../../ui/ModAvatar.vue";
import StatusFacts from "../../ui/StatusFacts.vue";
import DescriptionEditor from "./DescriptionEditor.vue";
import ListingImages from "./ListingImages.vue";
import ReviewUpload from "./ReviewUpload.vue";
import SteamCompareView from "./SteamCompareView.vue";
import SteamState from "./SteamState.vue";
import * as steam from "@services/steam/service";
import type { Listing } from "@services/steam/models";

/** The steps in order: each one's title and the line under it in the rail. */
const STEPS = [
  { value: "listing", title: "Listing and description", line: "What players see" },
  { value: "note", title: "Version and change note", line: "What this upload is" },
  { value: "review", title: "Review and upload", line: "Check, then send to Steam" },
];
/** The fields of a listing that are edited here until they are saved, with the words the state bar calls them. */
const EDITED = [
  { key: "title", words: "name" },
  { key: "tags", words: "tags" },
  { key: "description", words: "description" },
  { key: "version", words: "version" },
  { key: "supportedVersion", words: "supported game version" },
  { key: "changelog", words: "change note" },
] as const;
const KEYS = EDITED.map(({ key }) => key);
/** The fields that say what Steam has where the draft differs from it, each with Steam's value in the hint's words. */
const STEAM_VALUES = {
  title: (last: Listing) => last.title,
  version: (last: Listing) => last.version,
  tags: (last: Listing) => list(last.tags ?? []),
  description: (last: Listing) => (last.description ? "different text" : ""),
};

// a route can name the mod to show, and with compare "steam" open its compare with Steam
const { mod = "", compare = "" } = defineProps<{ id: string; mod?: string; compare?: string }>();

const workspace = useWorkspaceStore();
const queryCache = useQueryCache();
const selectedMod = shallowRef(mod);
const step = shallowRef("listing");
const comparing = shallowRef(compare === "steam");
const edits = ref<Record<string, Partial<Pick<Listing, (typeof KEYS)[number]>>>>({});
watch([() => mod, () => compare], ([nextMod, nextCompare]) => {
  selectedMod.value = nextMod;
  comparing.value = nextCompare === "steam";
});

const { data: state } = useQuery(stateQuery);
const { data: tags } = useQuery(() => ({ ...tagsQuery, enabled: workspace.isOpen() }));
const mods = computed(() => (state.value?.mods ?? []).filter((mod) => !mod.broken));
const modId = computed({
  get: () => (mods.value.some((mod) => mod.id === selectedMod.value) ? selectedMod.value : (mods.value[0]?.id ?? "")),
  set: (next: string) => (selectedMod.value = next),
});
const enabled = computed(() => workspace.isOpen() && !!modId.value);

const { data: listing } = useQuery(() => ({ ...listingQuery(modId.value), enabled: enabled.value }));
const { data: comparison, isLoading: asking } = useQuery(() => ({
  ...comparisonQuery(modId.value),
  enabled: enabled.value,
}));
const { data: upload } = useQuery(() => ({
  ...uploadQuery(modId.value),
  enabled: enabled.value && step.value === "review",
}));

const draft = computed(() => listing.value && { ...listing.value, ...edits.value[modId.value] });
const unsaved = computed(() =>
  EDITED.filter(({ key }) => !isEqual(draft.value?.[key], listing.value?.[key])).map(({ words }) => words),
);
// Steam's side is the last upload the compare holds; with none, each of its values is empty and no hint shows
const steamHas = computed(() =>
  mapValues(STEAM_VALUES, (text, field) => {
    const last = comparison.value?.last;
    const value = last && !isEqual(draft.value?.[field], last[field]) ? text(last) : "";
    return value && `Steam has ${value}`;
  }),
);
const heading = computed(() => STEPS.find((one) => one.value === step.value)?.title ?? "");
const following = computed(() => STEPS[STEPS.findIndex((one) => one.value === step.value) + 1]);
const items = computed<StepperItem[]>(() =>
  STEPS.map(({ value, title, line }) => ({
    value,
    title,
    description: value === "review" && unsaved.value.length ? "Save to workspace first" : line,
    disabled: value === "review" && unsaved.value.length > 0,
  })),
);
const bumps = computed(() => [
  { name: "minor", version: resolveVersion(draft.value?.version ?? "", 1) },
  { name: "hotfix", version: resolveVersion(draft.value?.version ?? "", 2) },
]);
const cells = computed(() => {
  const pending = unsaved.value.length > 0;
  const review = step.value === "review";
  const item = listing.value?.workshopId;
  return [
    {
      label: "Here, not saved",
      icon: pending ? "i-lucide-pencil" : "i-lucide-check",
      tone: pending ? "bg-(--orange-soft) text-warning" : "bg-(--green-soft) text-success",
      value: pending ? plural(unsaved.value.length, "unsaved change") : "Nothing unsaved",
      sub: pending ? list(unsaved.value) : "Every edit is in the workspace",
    },
    {
      label: "Saved in the workspace",
      icon: "i-lucide-save",
      tone: "bg-(--green-soft) text-success",
      value: `v${listing.value?.version}`,
      sub: review ? "This is what will be uploaded" : "What the next upload sends",
    },
    {
      label: "On Steam",
      icon: "i-lucide-cloud",
      tone: "bg-(--blue-soft) text-info",
      value: item || "Not published",
      sub: describeItem(review),
    },
  ];
});

const { mutate: save, isLoading: saving } = useMutation({
  mutation: (next: Listing) => steam.WriteListing(modId.value, next),
  onSuccess: discard,
});
const { mutate: fetchItem, isLoading: fetching } = useMutation({
  mutation: () => steam.FetchItem(modId.value),
  onSuccess: (fetched) => {
    queryCache.setQueryData(comparisonQuery(modId.value).key, fetched);
    void queryCache.invalidateQueries({ key: [...keys.release(modId.value), "file"] });
  },
});

/** setStep shows the step with the given value. It ignores a value that is not text. */
function setStep(value: string | number | undefined): void {
  if (typeof value === "string") step.value = value;
}

/**
 * describeItem returns the line under the mod's Workshop item in the state bar: how an item is made, what the upload
 * changes when reviewing, else Steam's details. While the compare is still asking Steam, or waits its turn to, it
 * says so. Only a compare that came back without Steam's details says they could not be read.
 */
function describeItem(review: boolean): string {
  if (!listing.value?.workshopId) return "Uploading creates the Workshop item";
  if (review && comparison.value) return uploadSentences(comparison.value).join(" ");
  if (comparison.value?.steam) return steamSentences(comparison.value.steam).join(" ");
  return asking.value ? "Asking Steam…" : "Steam's details could not be read";
}

/** discard drops the unsaved edits of the selected mod. */
function discard(): void {
  delete edits.value[modId.value];
}

/**
 * updateDraft keeps the fields of the given listing that differ from the saved one as the mod's unsaved edits, so a
 * field left alone follows the saved listing, as the description does when its source changes.
 */
function updateDraft(next: Listing): void {
  edits.value[modId.value] = pickBy(pick(next, KEYS), (value, key) => !isEqual(value, listing.value?.[key]));
}

/** resolveVersion returns the version with one more in its minor or hotfix number, as the position says: 1 or 2. */
function resolveVersion(version: string, position: 1 | 2): string {
  const [major = "0", minor = "0", hotfix = "0"] = version.split(".");
  const next = (Number.parseInt(position === 1 ? minor : hotfix) || 0) + 1;
  return position === 1 ? `${major}.${next}.0` : `${major}.${minor}.${next}`;
}
</script>

<template>
  <div class="flex h-full min-h-0 overflow-hidden">
    <section class="relative flex min-h-0 w-64 shrink-0 flex-col gap-1 border-e border-default bg-muted px-2.5 py-3.5">
      <span class="px-2.5 pb-1.5 text-11 font-medium text-dimmed">Mod</span>
      <UListbox
        v-model="modId"
        :items="mods"
        value-key="id"
        :ui="{
          root: 'gap-0 ring-0',
          content: 'max-h-none',
          group: 'flex flex-col gap-1 p-0',
          item: [
            'h-12 items-center gap-2.5 rounded-md px-2.5 text-13 text-toned before:rounded-md',
            'data-[state=checked]:bg-elevated data-[state=checked]:text-highlighted data-[state=checked]:ring data-[state=checked]:ring-inset data-[state=checked]:ring-accented',
          ],
          itemTrailingIcon: 'hidden',
        }"
      >
        <template #item="{ item }">
          <ModAvatar :mod="item" />
          <span class="flex min-w-0 grow flex-col">
            <span class="truncate font-medium">{{ item.descriptor.name }}</span>
            <span class="truncate text-11 text-dimmed">
              {{ item.descriptor.version }} · <SteamState :mod-id="item.id" />
            </span>
          </span>
        </template>
      </UListbox>
      <USeparator class="my-3 px-1" />
      <span class="px-2.5 pb-1.5 text-11 font-medium text-dimmed">Steps</span>
      <UStepper
        :model-value="step"
        :items="items"
        orientation="vertical"
        :linear="false"
        @update:model-value="setStep"
      />
    </section>

    <section v-if="draft && listing" class="flex min-w-0 grow flex-col overflow-hidden">
      <header class="flex h-14 shrink-0 items-center gap-3 border-b border-default ps-7 pe-5">
        <h1 class="text-16 whitespace-nowrap">{{ heading }}</h1>
        <span v-if="step === 'review'" class="truncate text-12 text-dimmed">
          Uploading replaces the files and listing of the Steam item below.
        </span>
        <template v-else>
          <span class="grow" />
          <UButton label="Discard changes" size="md" color="error" :disabled="!unsaved.length" @click="discard" />
          <!-- with nothing to save, the way forward takes Save's place -->
          <UButton
            v-if="unsaved.length || saving"
            label="Save to workspace"
            icon="i-lucide-save"
            size="md"
            color="primary"
            variant="solid"
            :disabled="!unsaved.length"
            :loading="saving"
            @click="save(draft)"
          />
          <UButton
            v-else-if="following"
            :label="following.title"
            trailing-icon="i-lucide-arrow-right"
            size="md"
            color="primary"
            variant="solid"
            @click="step = following.value"
          />
        </template>
      </header>

      <div class="mx-7 mt-4 flex shrink-0 items-stretch overflow-hidden rounded-lg border border-default bg-muted">
        <template v-for="(cell, index) in cells" :key="index">
          <UIcon v-if="index" name="i-lucide-chevron-right" class="size-3.5 shrink-0 self-center text-dimmed" />
          <div class="flex min-w-0 flex-1 basis-0 items-center gap-3 px-3.5 py-2.5">
            <span class="flex size-7.5 shrink-0 items-center justify-center rounded-lg" :class="cell.tone">
              <UIcon :name="cell.icon" class="size-3.5" />
            </span>
            <span class="flex min-w-0 grow flex-col">
              <span class="text-11 whitespace-nowrap text-dimmed">{{ cell.label }}</span>
              <span class="truncate text-13 font-semibold text-highlighted">{{ cell.value }}</span>
              <span class="truncate text-11 text-dimmed" :title="cell.sub">{{ cell.sub }}</span>
            </span>
            <UButton
              v-if="index === 2 && listing.workshopId && step !== 'review'"
              label="Compare"
              @click="comparing = true"
            />
          </div>
        </template>
      </div>

      <div v-if="step === 'listing'" class="flex min-h-0 grow gap-9 overflow-x-auto overflow-y-hidden px-7">
        <div class="@container flex min-w-100 flex-1 flex-col overflow-y-auto">
          <div class="grid grid-cols-1 gap-x-4 gap-y-3.5 border-b border-default py-4.5 @2xl:grid-cols-2">
            <UFormField label="Name" :help="steamHas.title">
              <UInput
                :model-value="draft.title"
                size="md"
                class="w-full"
                @update:model-value="updateDraft({ ...draft, title: `${$event}` })"
              />
            </UFormField>
            <UFormField label="Tags" :help="steamHas.tags">
              <UInputMenu
                :model-value="draft.tags ?? []"
                :items="tags ?? []"
                multiple
                create-item
                placeholder="Pick or add a tag"
                size="md"
                class="w-full"
                @update:model-value="updateDraft({ ...draft, tags: $event })"
                @create="updateDraft({ ...draft, tags: [...(draft.tags ?? []), $event] })"
              />
            </UFormField>
          </div>
          <ListingImages :mod-id="modId" :listing="listing" />
        </div>
        <DescriptionEditor
          :mod-id="modId"
          :listing="draft"
          :hint="steamHas.description"
          @update:listing="updateDraft"
        />
      </div>

      <div v-else-if="step === 'note'" class="flex min-h-0 grow flex-col overflow-y-auto px-7">
        <div class="flex w-170 max-w-full flex-col gap-3.5 py-4.5">
          <div class="grid grid-cols-2 gap-x-4">
            <UFormField label="Version" :help="steamHas.version">
              <div class="flex gap-1.5">
                <UInput
                  :model-value="draft.version"
                  size="md"
                  class="grow font-mono"
                  @update:model-value="updateDraft({ ...draft, version: `${$event}` })"
                />
                <UTooltip v-for="next in bumps" :key="next.name" :text="`The next ${next.name} version`">
                  <UButton
                    :label="next.version"
                    icon="i-lucide-chevron-up"
                    size="sm"
                    class="shrink-0 self-center font-mono"
                    @click="updateDraft({ ...draft, version: next.version })"
                  />
                </UTooltip>
              </div>
            </UFormField>
            <UFormField label="Supported game version" :help="`Installed game is ${state?.install.version}`">
              <UInput
                :model-value="draft.supportedVersion"
                size="md"
                class="w-full font-mono"
                @update:model-value="updateDraft({ ...draft, supportedVersion: `${$event}` })"
              />
            </UFormField>
          </div>
          <UFormField
            label="Change note"
            help="Shown in the Steam change log. BBCode, kept as workshop/changelog.bbcode until the next note."
          >
            <template #hint>
              <UButton
                label="Draft"
                variant="link"
                class="p-0 text-12"
                @click="updateDraft({ ...draft, changelog: `Version ${draft.version}\n` })"
              />
            </template>
            <UTextarea
              :model-value="draft.changelog"
              :rows="12"
              class="w-full"
              @update:model-value="updateDraft({ ...draft, changelog: `${$event}` })"
            />
          </UFormField>
        </div>
      </div>

      <ReviewUpload
        v-else-if="upload && comparison"
        :mod-id="modId"
        :upload="upload"
        :comparison="comparison"
        @back="step = 'note'"
      />
    </section>
    <p v-else-if="!mods.length" class="p-7 text-13 text-dimmed">This workspace has no mod with a folder to publish.</p>

    <UModal v-model:open="comparing" title="Compared with Steam" fullscreen>
      <template #body>
        <SteamCompareView v-if="listing && comparison" :mod-id="modId" :listing="listing" :comparison="comparison" />
      </template>
      <template #footer>
        <span class="grow" />
        <UButton
          label="Download Steam's copy to compare"
          icon="i-lucide-download"
          :loading="fetching"
          @click="fetchItem()"
        />
      </template>
    </UModal>

    <StatusFacts
      v-if="listing"
      :facts="[
        listing.workshopId
          ? { label: `${step === 'review' ? 'Steam' : 'Workshop'} item`, value: listing.workshopId }
          : { label: 'Not on the Workshop yet', value: '' },
        ...(step === 'review' ? [] : [{ label: 'Nothing is uploaded until the review step', value: '' }]),
      ]"
    />
  </div>
</template>
