<script setup lang="ts">
/**
 * Publish: a mod's listing and description, then its version and change note, edited
 * here and saved into the mod, then reviewed and uploaded to Steam; the comparison with what Steam has opens over
 * any step. Each mod keeps its unsaved edits while another is shown.
 */
import { computed, ref, shallowRef, watchEffect } from "vue";
import { useRouter } from "vue-router";
import { useMutation, useQuery, useQueryCache } from "@pinia/colada";
import type { Listing } from "@services/models";
import { SaveListing } from "@services/releaseservice";
import SteamCompareView from "./SteamCompareView.vue";
import { compareQuery, languageQuery, listingQuery, releaseKeys, summaryQuery } from "../../queries";
import { useWorkspaceStore } from "../../stores/workspace";
import DescriptionEditor from "./DescriptionEditor.vue";
import ListingImages from "./ListingImages.vue";
import PublishState from "./PublishState.vue";
import ReleaseModRail from "./ReleaseModRail.vue";
import ReviewUpload from "./ReviewUpload.vue";
import StatusFacts from "../../ui/StatusFacts.vue";

const { id, step: asked = "" } = defineProps<{ id: string; step?: string }>();

const router = useRouter();
const store = useWorkspaceStore();
const queryCache = useQueryCache();

/** The three steps in order: each one's title, the heading's and the rail's, and the rail's line under it. */
const STEPS = [
  { value: "listing", title: "Listing and description", line: "What players see" },
  { value: "release", title: "Version and change note", line: "What this upload is" },
  { value: "review", title: "Review and upload", line: "Check, then send to Steam" },
] as const;
type Step = (typeof STEPS)[number]["value"];
const titleOf = (value: Step) => STEPS.find((one) => one.value === value)?.title ?? "";

const step = computed<Step>({
  get: () => (asked === "release" || asked === "review" ? asked : "listing"),
  set: (next) => void router.replace({ query: { step: next } }),
});
const next = computed<Step>(() => (step.value === "listing" ? "release" : "review"));

const mods = computed(() => store.workspaceMods.filter((mod) => !mod.isBroken && mod.path));
const chosen = shallowRef("");
const modId = computed({
  get: () => (mods.value.some((mod) => mod.id === chosen.value) ? chosen.value : (mods.value[0]?.id ?? "")),
  set: (next: string) => (chosen.value = next),
});
const mod = computed(() => mods.value.find((each) => each.id === modId.value));
const ids = computed(() => ({ workspaceId: id, modId: modId.value }));
const { data: loaded } = useQuery(() => listingQuery(ids.value));
const { data: summary } = useQuery(() => summaryQuery(ids.value));
const { data: steam } = useQuery(() => compareQuery(ids.value));
const { data: language } = useQuery(() => languageQuery(id));

/** The fields edited here until a save, by what the state bar calls them; images and links save at once. */
const EDITED = {
  name: "name",
  version: "version",
  supportedVersion: "supported game version",
  tags: "tags",
  readmeMd: "description",
  readmeBbcode: "description",
  changeNote: "change note",
} as const;
type Draft = Pick<Listing, keyof typeof EDITED>;
function draftOf(listing: Listing): Draft {
  const { name, version, supportedVersion, tags, readmeMd, readmeBbcode, changeNote } = listing;
  return { name, version, supportedVersion, tags: [...(tags ?? [])], readmeMd, readmeBbcode, changeNote };
}
const drafts = ref<Record<string, Draft>>({});
// a mod's draft starts as its listing, once
watchEffect(() => {
  if (loaded.value && !drafts.value[loaded.value.modId]) drafts.value[loaded.value.modId] = draftOf(loaded.value);
});
const draft = computed(() => drafts.value[modId.value]);
const edited = computed(() => (loaded.value && draft.value ? { ...loaded.value, ...draft.value } : undefined));
const unsaved = computed(() => {
  if (!loaded.value || !draft.value) return [];
  const saved = draftOf(loaded.value);
  const changed = (Object.keys(EDITED) as (keyof Draft)[]).filter(
    (key) => JSON.stringify(draft.value?.[key]) !== JSON.stringify(saved[key]),
  );
  return [...new Set(changed.map((key) => EDITED[key]))];
});
function discard(): void {
  if (loaded.value) drafts.value[loaded.value.modId] = draftOf(loaded.value);
}
const { mutate: save, isLoading: saving } = useMutation({
  mutation: (listing: Listing) => SaveListing(listing),
  onSettled: () => queryCache.invalidateQueries({ key: releaseKeys.mod(id, modId.value) }),
});

// the version with its minor or hotfix number one higher ("3beta" counts as 3), every number after it zeroed
function bumped(at: 1 | 2): string {
  const parts = (draft.value?.version.trim() || "0.0.0").split(".");
  while (parts.length < 3) parts.push("0");
  parts[at] = String((parseInt(parts[at] ?? "") || 0) + 1);
  return parts.map((part, i) => (i > at && /^\d/.test(part) ? "0" : part)).join(".");
}
const bumps = computed(() => [
  { name: "minor", version: bumped(1) },
  { name: "hotfix", version: bumped(2) },
]);
function draftNote(): void {
  if (draft.value) draft.value.changeNote = `Version ${draft.value.version || "0.0.0"}\n`;
}

const comparison = useOverlay().create(SteamCompareView);
function compare(): void {
  if (loaded.value) void comparison.open({ listing: loaded.value });
}
</script>

<template>
  <div class="flex h-full min-h-0 overflow-hidden">
    <ReleaseModRail
      v-model:mod="modId"
      v-model:step="step"
      :workspace-id="id"
      :mods="mods"
      :saved="!unsaved.length"
      :steps="STEPS"
    />
    <section v-if="loaded && draft && edited && summary && mod" class="flex min-w-0 grow flex-col overflow-hidden">
      <header class="flex h-14 shrink-0 items-center gap-3 border-b border-default ps-7 pe-5">
        <h1 class="text-16 whitespace-nowrap">{{ titleOf(step) }}</h1>
        <span v-if="step === 'review'" class="truncate text-12 text-dimmed"
          >Uploading replaces the files and listing of the Steam item below.</span
        >
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
            :loading="saving"
            @click="save(edited)"
          />
          <UButton
            v-else
            :label="titleOf(next)"
            trailing-icon="i-lucide-arrow-right"
            size="md"
            color="primary"
            variant="solid"
            @click="step = next"
          />
        </template>
      </header>

      <div class="shrink-0 px-7 pt-4">
        <PublishState
          :unsaved="unsaved"
          :listing="loaded"
          :steam="steam"
          :review="step === 'review'"
          @compare="compare"
        />
      </div>

      <div v-if="step === 'listing'" class="flex min-h-0 grow gap-9 overflow-x-auto overflow-y-hidden px-7">
        <!-- the fields and images take the width the description leaves: name and tags side by side -->
        <div class="@container flex min-w-100 flex-1 flex-col overflow-y-auto">
          <div class="grid grid-cols-1 gap-x-4 gap-y-3.5 border-b border-default py-4.5 @2xl:grid-cols-2">
            <UFormField label="Name">
              <UInput v-model="draft.name" size="md" class="w-full" />
            </UFormField>
            <UFormField label="Tags">
              <UInputTags v-model="draft.tags" placeholder="Add a tag" size="md" class="w-full" />
            </UFormField>
          </div>
          <ListingImages :listing="loaded" :mod="mod" />
        </div>
        <DescriptionEditor
          v-model:markdown="draft.readmeMd"
          v-model:bbcode="draft.readmeBbcode"
          :listing="loaded"
          :summary="summary"
        />
      </div>
      <!-- what this upload is: its version, the game version it supports and its change note -->
      <div v-else-if="step === 'release'" class="flex min-h-0 grow flex-col overflow-y-auto px-7">
        <div class="flex w-170 max-w-full flex-col gap-3.5 py-4.5">
          <div class="grid grid-cols-2 gap-x-4">
            <UFormField label="Version" :help="steam?.from ? `Steam has ${steam.version}` : undefined">
              <div class="flex gap-1.5">
                <UInput v-model="draft.version" size="md" class="grow font-mono" />
                <UTooltip v-for="bump in bumps" :key="bump.name" :text="`The next ${bump.name} version`">
                  <UButton
                    :label="bump.version"
                    icon="i-lucide-chevron-up"
                    size="sm"
                    :aria-label="`The next ${bump.name} version, ${bump.version}`"
                    class="shrink-0 self-center font-mono"
                    @click="draft.version = bump.version"
                  />
                </UTooltip>
              </div>
            </UFormField>
            <UFormField
              label="Supported game version"
              :help="language?.gameVersion ? `Installed game is ${language.gameVersion}` : undefined"
            >
              <UInput v-model="draft.supportedVersion" size="md" class="w-full font-mono" />
            </UFormField>
          </div>
          <UFormField
            label="Change note"
            help="Shown in the Steam change log. BBCode, kept as workshop/changelog.bbcode until the next note."
          >
            <template #hint>
              <UButton label="Draft" variant="link" class="p-0 text-12" @click="draftNote()" />
            </template>
            <UTextarea v-model="draft.changeNote" :rows="12" class="w-full" />
          </UFormField>
        </div>
      </div>
      <ReviewUpload
        v-else
        :listing="edited"
        :summary="summary"
        :steam="steam"
        :saved="!unsaved.length && !saving"
        @back="step = 'release'"
        @compare="compare"
      />
    </section>
    <p v-else-if="!mods.length" class="p-7 text-13 text-dimmed">This workspace has no mod with a folder to publish.</p>

    <StatusFacts v-if="loaded">
      <span>{{
        loaded.remoteFileId
          ? `${step === "review" ? "Steam" : "Workshop"} item ${loaded.remoteFileId}`
          : "Not on the Workshop yet"
      }}</span>
      <span v-if="step !== 'review'">Nothing is uploaded until step 3</span>
    </StatusFacts>
  </div>
</template>
