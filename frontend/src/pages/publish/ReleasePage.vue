<!--
  The Publish page shows a mod's Workshop listing, or its compare with Steam, beside the rail that uploads it.
  Edits are saved to the mod's files when the typing stops.
-->
<script setup lang="ts">
import { computed, onBeforeUnmount, shallowRef, watch } from "vue";
import { useLocalStorage, useTimeoutFn } from "@vueuse/core";
import { useMutation, useQuery, useQueryCache } from "@pinia/colada";
import { Browser } from "@wailsio/runtime";
import type { DropdownMenuItem } from "@nuxt/ui";

import { describeDifference } from "../../lib/format";
import { comparisonQuery, listingQuery, listingsQuery, stateQuery } from "../../queries";
import { useWorkspaceStore } from "../../stores/workspace";
import FileSelector from "../../ui/FileSelector.vue";
import ModAvatar from "../../ui/ModAvatar.vue";
import StatusFacts from "../../ui/StatusFacts.vue";
import DescriptionEditor from "./DescriptionEditor.vue";
import ListingImages from "./ListingImages.vue";
import OlderListing from "./OlderListing.vue";
import ReleaseRail from "./ReleaseRail.vue";
import SteamCompareView from "./SteamCompareView.vue";
import UploadFiles from "./UploadFiles.vue";
import WorkshopPage from "./WorkshopPage.vue";
import { AGREEMENT_URL, toPageURL, useZip } from "./workshop";
import * as app from "@services/app/service";
import * as steam from "@services/steam/service";
import type { Listing } from "@services/steam/models";
import type { Mod } from "@services/workspace/models";

/** Unsaved is a listing as typed, not yet written to its mod. */
interface Unsaved {
  modId: string;
  key: string;
  listing: Listing;
}

/** STATUSES holds the header badge of each comparison status. */
const STATUSES: Record<string, { label: string; color: "success" | "neutral" }> = {
  changed: { label: "Differs from Steam", color: "neutral" },
  same: { label: "Same as on Steam", color: "success" },
  new: { label: "Not on the Workshop yet", color: "neutral" },
  unknown: { label: "Not compared with Steam", color: "neutral" },
};
/** ASKING is the badge shown while Steam is asked. */
const ASKING = { label: "Asking Steam…", color: "neutral" } as const;
/** TWO_LINES lets a picker row grow to show its second line. */
const TWO_LINES = "h-auto py-1.5";
/** SAVE_DELAY is how long after the last edit the listing is written, in milliseconds. */
const SAVE_DELAY = 400;

// The route may name the mod and listing to show, and ask for the compare with Steam.
const {
  id,
  mod = "",
  compare = "",
  listing: listingProp = "",
} = defineProps<{ id: string; mod?: string; compare?: string; listing?: string }>();

const workspace = useWorkspaceStore();
const queryCache = useQueryCache();
/** The mod last shown in each workspace, by workspace id. */
const shownMods = useLocalStorage<Record<string, string>>("pmt.publish.mods", {});
// The route's mod, else the one last shown here. One the workspace no longer has gives way to its first mod.
const selectedMod = shallowRef("");
// "" is the mod's own listing. An older listing's key is its game version.
const listingKey = shallowRef("");
const comparing = shallowRef(false);
const picked = shallowRef("");
const unsaved = shallowRef<Unsaved>();
const olderMode = shallowRef<"create" | "import">("create");
const older = shallowRef(false);
const linking = shallowRef(false);
const workshopItem = shallowRef("");
const filesOpen = shallowRef(false);
// The page starts from the route, and another route or workspace starts over, since a listing picked here is the last
// one's.
watch(
  [() => mod, () => listingProp, () => compare, () => id],
  ([nextMod, nextKey, nextCompare, nextId]) => {
    selectedMod.value = nextMod || (shownMods.value[nextId] ?? "");
    listingKey.value = nextKey;
    comparing.value = nextCompare === "steam";
    picked.value = "";
  },
  { immediate: true },
);

const { data: state } = useQuery(stateQuery);
const mods = computed(() => (state.value?.mods ?? []).filter((one) => !one.broken));
const modId = computed(() =>
  mods.value.some((one) => one.id === selectedMod.value) ? selectedMod.value : (mods.value[0]?.id ?? ""),
);
const current = computed(() => mods.value.find((one) => one.id === modId.value));
const target = computed(() => ({ modId: modId.value, key: listingKey.value }));
const enabled = computed(() => workspace.isOpen() && !!modId.value);
// The shown mod is kept once this workspace's state has come, not while the last one's still shows.
watch(
  [modId, () => state.value?.id],
  ([shown, stateId]) => {
    if (shown && stateId === id) shownMods.value = { ...shownMods.value, [id]: shown };
  },
  { immediate: true },
);

const {
  data: saved,
  error: listingError,
  isLoading: loading,
} = useQuery(() => ({ ...listingQuery(target.value), enabled: enabled.value }));
const { data: listings } = useQuery(() => ({ ...listingsQuery(modId.value), enabled: enabled.value }));
const { data: comparison, isLoading: asking } = useQuery(() => ({
  ...comparisonQuery(target.value),
  enabled: enabled.value,
}));

// The picked difference while the comparison still holds it, else the first one. Both the rail and the compare show it.
const shown = computed(() => {
  const ids = [...(comparison.value?.page ?? []), ...(comparison.value?.files ?? [])].map(
    (difference) => describeDifference(difference).id,
  );
  return ids.find((one) => one === picked.value) ?? ids[0] ?? "";
});

// The listing as typed while it is unsaved, else as saved.
const draft = computed(() =>
  unsaved.value?.modId === modId.value && unsaved.value.key === listingKey.value ? unsaved.value.listing : saved.value,
);
// A listing not yet on the Workshop gets no badge.
const badge = computed(() => {
  if (!comparison.value) return asking.value ? ASKING : undefined;
  return comparison.value.status === "new" ? undefined : STATUSES[comparison.value.status];
});
const showCompare = computed(() => comparing.value && !!comparison.value);
const picker = computed(
  () =>
    [
      ...(mods.value.length > 1
        ? [
            [
              { type: "label" as const, label: "Mods" },
              ...mods.value.map((one) => ({
                label: one.descriptor.name,
                description: describeMod(one),
                class: TWO_LINES,
                slot: "mod" as const,
                mod: one,
                active: one.id === modId.value,
                onSelect: () => openListing(one.id, ""),
              })),
            ],
          ]
        : []),
      [
        { type: "label" as const, label: "Listings of this mod" },
        ...(listings.value ?? []).map((one) => ({
          label: one.title,
          description: describeListing(one),
          class: TWO_LINES,
          slot: "mod" as const,
          mod: current.value,
          active: one.key === listingKey.value,
          onSelect: () => openListing(modId.value, one.key),
        })),
      ],
      [
        { label: "New listing for an older game version…", icon: "i-lucide-plus", onSelect: () => openOlder("create") },
        {
          label: "Import a listing from the Workshop…",
          icon: "i-lucide-download",
          onSelect: () => openOlder("import"),
        },
      ],
    ] satisfies DropdownMenuItem[][],
);
const menu = computed<DropdownMenuItem[][]>(() => [
  [
    ...(draft.value?.workshopId
      ? [
          {
            label: "Open the Steam page",
            icon: "i-lucide-external-link",
            onSelect: () => void Browser.OpenURL(toPageURL(draft.value?.workshopId ?? "")),
          },
        ]
      : []),
    { label: "Link to another Workshop item…", icon: "i-lucide-link", onSelect: () => (linking.value = true) },
    {
      label: "New listing for an older game version…",
      icon: "i-lucide-history",
      onSelect: () => openOlder("create"),
    },
  ],
  [
    { label: "Files never uploaded…", icon: "i-lucide-file-x", onSelect: () => (filesOpen.value = true) },
    {
      label: "Save the mod as a zip…",
      icon: "i-lucide-file-archive",
      onSelect: () => writeZip(current.value?.descriptor.name ?? ""),
    },
    { label: "Open the mod's folder", icon: "i-lucide-folder-open", onSelect: () => showFolder() },
  ],
  [
    {
      label: "Steam Workshop agreement",
      icon: "i-lucide-scroll-text",
      onSelect: () => void Browser.OpenURL(AGREEMENT_URL),
    },
  ],
]);

const { start: schedule, stop: cancel } = useTimeoutFn(save, SAVE_DELAY, { immediate: false });
// The written listing is cached at once, so the page keeps its text until release:changed reloads it.
const {
  mutate: write,
  isLoading: writing,
  error: writeError,
} = useMutation({
  mutation: (edit: Unsaved) => steam.WriteListing(edit.modId, edit.key, edit.listing),
  onSuccess: (_, edit) => {
    queryCache.setQueryData(listingQuery(edit).key, edit.listing);
    if (unsaved.value === edit) unsaved.value = undefined;
  },
});
const { mutate: writeZip } = useZip(modId);
const { mutate: showFolder } = useMutation({ mutation: () => app.ShowFolder(current.value?.dir ?? "") });
const { mutate: link, isLoading: linkingItem } = useMutation({
  mutation: (text: string) => steam.LinkItem(modId.value, listingKey.value, text),
  onSuccess: () => {
    linking.value = false;
    workshopItem.value = "";
  },
});
// The status bar says whether the edits are written.
const facts = computed(() => {
  const saving = unsaved.value || writing.value ? "Saving…" : "Saved in the mod";
  return [writeError.value ? { label: "Not saved", value: writeError.value.message } : { label: saving, value: "" }];
});

// An unsaved edit is written before another listing shows or the page closes.
watch(target, flush);
onBeforeUnmount(flush);

/** edit holds a listing as typed and writes it when the typing stops. */
function edit(next: Listing): void {
  unsaved.value = { modId: modId.value, key: listingKey.value, listing: next };
  schedule();
}

/** save writes the unsaved edit, if there is one. */
function save(): void {
  if (unsaved.value) write(unsaved.value);
}

/** flush writes the unsaved edit at once. */
function flush(): void {
  cancel();
  save();
}

/** submitLink links the Workshop item typed in the dialog. It does nothing if the field is blank. */
function submitLink(): void {
  if (workshopItem.value.trim()) link(workshopItem.value);
}

/** openListing shows one listing of a mod and leaves the compare. */
function openListing(mod: string, key: string): void {
  selectedMod.value = mod;
  listingKey.value = key;
  comparing.value = false;
  picked.value = "";
}

/** openOlder opens the dialog that adds an older listing, made from a zip or imported. */
function openOlder(mode: "create" | "import"): void {
  olderMode.value = mode;
  older.value = true;
}

/** describeMod returns a mod's version for the picker, with its state on Steam if it was already compared. */
function describeMod(one: Mod): string {
  const asked = queryCache.getQueryData(comparisonQuery({ modId: one.id, key: "" }).key);
  return [one.descriptor.version, asked && STATUSES[asked.status]?.label].filter(Boolean).join(" · ");
}

/** describeListing returns a listing's second line in the picker. */
function describeListing(one: Listing): string {
  const where = one.source ? `from ${one.source}` : "from the mod's folder";
  return `For game ${one.supportedVersion} · ${one.workshopId ? `item ${one.workshopId}` : "not on the Workshop"} · ${where}`;
}

/** keepFocus stops a closing menu from taking the focus back from a dialog it opened. */
function keepFocus(event: Event): void {
  event.preventDefault();
}

/** openCompare shows the compare with Steam at one difference. */
function openCompare(id: string): void {
  picked.value = id;
  comparing.value = true;
}
</script>

<template>
  <div class="flex h-full min-h-0 overflow-hidden">
    <template v-if="current">
      <section class="flex min-w-0 grow flex-col">
        <header class="flex h-14 shrink-0 items-center gap-2.5 border-b border-default ps-4.5 pe-5">
          <UDropdownMenu
            :items="picker"
            :content="{ align: 'start', onCloseAutoFocus: keepFocus }"
            :ui="{ content: 'w-112' }"
          >
            <template #mod-leading="{ item: row }">
              <ModAvatar v-if="row.mod" :mod="row.mod" />
            </template>
            <UButton
              color="neutral"
              variant="ghost"
              trailing-icon="i-lucide-chevron-down"
              class="h-9 min-w-0 gap-2.5 ps-1"
            >
              <ModAvatar :mod="current" />
              <span class="flex min-w-0 flex-col items-start">
                <span class="truncate text-13 font-semibold text-highlighted">
                  {{ draft?.title ?? current.descriptor.name }}
                </span>
                <span v-if="listingKey && draft" class="text-11 font-normal text-dimmed">
                  For game {{ draft.supportedVersion }} · an older version
                </span>
              </span>
            </UButton>
          </UDropdownMenu>
          <UBadge v-if="badge" :label="badge.label" :color="badge.color" variant="subtle" class="shrink-0" />
          <span class="grow" />
          <UDropdownMenu
            :items="menu"
            :content="{ align: 'end', onCloseAutoFocus: keepFocus }"
            :ui="{ content: 'w-80' }"
          >
            <UTooltip text="Steam page, links, older versions and the mod's files">
              <UButton
                icon="i-lucide-ellipsis"
                square
                class="shrink-0"
                aria-label="Steam page, links, older versions and the mod's files"
              />
            </UTooltip>
          </UDropdownMenu>
        </header>

        <SteamCompareView
          v-if="draft && comparison && showCompare"
          :picked="shown"
          :mod-id="modId"
          :listing-key="listingKey"
          :listing="draft"
          :comparison="comparison"
          @update:picked="picked = $event"
          @back="comparing = false"
        />
        <div v-else-if="draft" class="flex min-h-0 grow flex-col gap-4 overflow-y-auto px-7 pt-5.5 pb-8">
          <div
            v-if="listingKey"
            class="flex max-w-210 items-center gap-2.5 rounded-lg border border-default bg-muted px-3 py-2.5 text-13"
          >
            <UIcon name="i-lucide-info" class="size-4 shrink-0 text-dimmed" />
            <span class="grow">
              A separate Workshop item for players on game {{ draft.supportedVersion }}. Its files come from
              <span class="font-mono text-12">{{ draft.source }}</span
              >, not the mod's folder.
            </span>
            <FileSelector
              :path="draft.source"
              :filter="['*.zip']"
              class="shrink-0"
              @update:path="edit({ ...draft, source: $event })"
            >
              <template #default="{ browse }">
                <UButton label="Change the source" @click="browse" />
              </template>
            </FileSelector>
          </div>
          <div class="flex max-w-210 flex-col gap-4.5">
            <WorkshopPage :listing="draft" @update:listing="edit" />
            <ListingImages :mod-id="modId" :listing-key="listingKey" :listing="draft" :comparison="comparison" />
            <DescriptionEditor
              :listing="draft"
              :mod-id="modId"
              :listing-key="listingKey"
              :comparison="comparison"
              @update:listing="edit"
            />
          </div>
        </div>
        <UAlert
          v-else-if="listingError"
          color="error"
          variant="subtle"
          title="The listing could not be read. Fix its files in the IDE, or pick another from the mod's name."
          :description="listingError.message"
          class="m-4 max-w-210"
        />
        <p v-else-if="loading" class="px-7 pt-5.5 text-13 text-dimmed">Reading the listing.</p>
      </section>

      <ReleaseRail
        v-if="draft"
        :mod-id="modId"
        :listing-key="listingKey"
        :listing="draft"
        :comparing="showCompare"
        :picked="shown"
        @update:listing="edit"
        @compare="openCompare"
        @link="linking = true"
      />
    </template>
    <p v-else-if="state && !mods.length" class="p-7 text-13 text-dimmed">
      This workspace has no mod with a folder to publish.
    </p>

    <OlderListing
      v-model:open="older"
      :mod-id="modId"
      :mod-title="current?.descriptor.name ?? ''"
      :mode="olderMode"
      @added="listingKey = $event"
    />
    <UploadFiles v-if="current" v-model:open="filesOpen" :mod="current" />
    <UModal
      v-model:open="linking"
      title="Link to another Workshop item"
      description="The listing takes the Workshop item you name as its own. Uploads update that item."
      :ui="{ content: 'h-64 sm:max-w-lg' }"
    >
      <template #body>
        <form class="flex flex-col gap-4" @submit.prevent="submitLink">
          <UFormField label="Workshop item" help="The item's id or the address of its Workshop page.">
            <UInput
              v-model="workshopItem"
              placeholder="The item's id or its address"
              class="w-full font-mono"
              autofocus
            />
          </UFormField>
        </form>
      </template>
      <template #footer>
        <span class="grow" />
        <UButton label="Cancel" @click="linking = false" />
        <UButton
          label="Link"
          color="primary"
          variant="solid"
          :disabled="!workshopItem.trim()"
          :loading="linkingItem"
          @click="link(workshopItem)"
        />
      </template>
    </UModal>

    <StatusFacts v-if="draft && !showCompare" :facts="facts" />
  </div>
</template>
