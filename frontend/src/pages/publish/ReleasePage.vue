<!-- The Publish page: a mod's Workshop listing or its compare with Steam, beside the rail that uploads it. -->
<script setup lang="ts">
import { computed, onBeforeUnmount, shallowRef, watch } from "vue";
import { useLocalStorage, useTimeoutFn } from "@vueuse/core";
import { useMutation, useQuery, useQueryCache } from "@pinia/colada";
import { Browser } from "@wailsio/runtime";
import type { DropdownMenuItem } from "@nuxt/ui";

import { toDifferenceId } from "../../lib/format";
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
import * as workspaceApi from "@services/workspace/service";
import type { Listing } from "@services/steam/models";
import type { Mod } from "@services/workspace/models";

/** Unsaved is a listing as typed, not yet written to its mod. */
interface Unsaved {
  modId: string;
  key: string;
  listing: Listing;
}

/** A Badge is the header's mark of what the page shows. */
interface Badge {
  label: string;
  color: "success" | "neutral";
  icon?: string;
}

/** STATUSES holds the header badge of each comparison status. */
const STATUSES: Record<string, Badge> = {
  changed: { label: "Differs from Steam", color: "neutral" },
  same: { label: "Same as on Steam", color: "success" },
  new: { label: "Not on the Workshop yet", color: "neutral" },
  unknown: { label: "Not compared with Steam", color: "neutral" },
};
const ASKING: Badge = { label: "Asking Steam…", color: "neutral" };
/** EXTERNAL_BADGE is the header badge of an external mod; a Steam copy says whose copy it is. */
const EXTERNAL_BADGE: Badge = { label: "External mod", color: "neutral", icon: "i-lucide-package" };
const STEAM_COPY_BADGE: Badge = { ...EXTERNAL_BADGE, label: "External mod · Steam's copy" };
/** A Notice is the card over the page of a mod PMT does not publish: why, how to publish a version of one's own. */
interface Notice {
  title: string;
  why: string;
  how: string;
  /** The button that makes the mod an external mod; none if the mod is one already. */
  button?: string;
}

/** NOTICES holds the notice of each case. */
const NOTICES: Record<"steam" | "external" | "foreign", Notice> = {
  steam: {
    title: "Steam's copy of a Workshop mod",
    why: "This is Steam's copy of a Workshop mod you subscribe to. PMT never publishes it.",
    how: "To make your own version, copy its folder, or what's in it, to wherever you develop, add the copy to your workspace as a mod of yours, and give it a new name.",
  },
  external: {
    title: "An external mod",
    why: "This is an external mod, so PMT does not publish it. If it is yours, switch off External mod on its card in Settings.",
    how: "To publish your own version of someone's mod, give it a new name.",
  },
  foreign: {
    title: "Someone else's Workshop item",
    why: "This mod's Workshop item belongs to another Steam user, so PMT cannot update it.",
    how: "To publish it as your own, give it a new name.",
    button: "Make it an external mod",
  },
};
/** TWO_LINES lets a picker row grow to show its second line. */
const TWO_LINES = "h-auto py-1.5";
/** BLURRED is how the page under a notice looks: dimmed, blurred and out of reach. */
const BLURRED = "pointer-events-none opacity-45 blur-sm select-none";
/** SAVE_DELAY is how long after the last edit the listing is written, in milliseconds. */
const SAVE_DELAY = 400;

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
// Another route or workspace starts over, since the picks made here belong to the last one.
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
const mods = computed(() => workspace.listMods().filter((workspaceMod) => !workspaceMod.broken));
const ownMods = computed(() => workspace.listOwnMods());
const externalMods = computed(() => mods.value.filter((workspaceMod) => workspaceMod.external));
const modId = computed(() =>
  mods.value.some((candidate) => candidate.id === selectedMod.value)
    ? selectedMod.value
    : ((ownMods.value[0] ?? mods.value[0])?.id ?? ""),
);
const current = computed(() => mods.value.find((candidate) => candidate.id === modId.value));
const target = computed(() => ({ modId: modId.value, key: listingKey.value }));
const enabled = computed(() => workspace.isOpen() && !!modId.value);
// Steam is never asked about an external mod, and it has no listings of its own.
const publishable = computed(() => enabled.value && !current.value?.external);
// The shown mod is kept only once this workspace's state has come, not while the last one's still shows.
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
const { data: listings } = useQuery(() => ({ ...listingsQuery(modId.value), enabled: publishable.value }));
const { data: comparison, isLoading: asking } = useQuery(() => ({
  ...comparisonQuery(target.value),
  enabled: publishable.value,
}));

const shown = computed(() => {
  const ids = [...(comparison.value?.page ?? []), ...(comparison.value?.files ?? [])].map(toDifferenceId);
  return ids.includes(picked.value) ? picked.value : (ids[0] ?? "");
});

const draft = computed(() =>
  unsaved.value?.modId === modId.value && unsaved.value.key === listingKey.value ? unsaved.value.listing : saved.value,
);
const badge = computed((): Badge | undefined => {
  if (current.value?.external) return current.value.workshop ? STEAM_COPY_BADGE : EXTERNAL_BADGE;
  if (!comparison.value) return asking.value ? ASKING : undefined;
  return comparison.value.status === "new" ? undefined : STATUSES[comparison.value.status];
});
// A mod PMT does not publish: external, or a mod of the user's whose Workshop item another Steam user owns.
const notice = computed(() => {
  if (current.value?.workshop) return NOTICES.steam;
  if (current.value?.external) return NOTICES.external;
  return comparison.value?.steam?.owned === false ? NOTICES.foreign : undefined;
});
const showCompare = computed(() => comparing.value && !!comparison.value);
const picker = computed(
  () =>
    [
      ...(mods.value.length > 1
        ? [
            [{ type: "label" as const, label: "Your mods" }, ...ownMods.value.map(toModItem)],
            ...(externalMods.value.length
              ? [
                  [
                    { type: "label" as const, label: "External mods · never published" },
                    ...externalMods.value.map(toModItem),
                  ],
                ]
              : []),
          ]
        : []),
      ...(current.value?.external
        ? []
        : [
            [
              { type: "label" as const, label: "Listings of this mod" },
              ...(listings.value ?? []).map((modListing) => ({
                label: modListing.title,
                description: describeListing(modListing),
                class: TWO_LINES,
                slot: "mod" as const,
                mod: current.value,
                active: modListing.key === listingKey.value,
                onSelect: () => openListing(modId.value, modListing.key),
              })),
            ],
            [
              {
                label: "New listing for an older game version…",
                icon: "i-lucide-plus",
                onSelect: () => openOlder("create"),
              },
              {
                label: "Import a listing from the Workshop…",
                icon: "i-lucide-download",
                onSelect: () => openOlder("import"),
              },
            ],
          ]),
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
    { label: "What an upload sends…", icon: "i-lucide-files", onSelect: () => (filesOpen.value = true) },
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
  reset: resetWrite,
} = useMutation({
  mutation: (edit: Unsaved) => steam.WriteListing(edit.modId, edit.key, edit.listing),
  onSuccess: (_, edit) => {
    queryCache.setQueryData(listingQuery(edit).key, edit.listing);
    if (unsaved.value === edit) unsaved.value = undefined;
  },
});
const { mutate: makeExternal } = useMutation({ mutation: () => workspaceApi.SetExternal(id, modId.value, true) });
const { mutate: writeZip } = useZip(modId);
const { mutate: showFolder } = useMutation({ mutation: () => app.ShowFolder(current.value?.dir ?? "") });
const { mutate: link, isLoading: linkingItem } = useMutation({
  mutation: (text: string) => steam.LinkItem(modId.value, listingKey.value, text),
  onSuccess: () => {
    linking.value = false;
    workshopItem.value = "";
  },
});
const facts = computed(() => {
  if (writeError.value) return [{ label: "Not saved", value: writeError.value.message }];
  return unsaved.value || writing.value ? [{ label: "Saving…", value: "" }] : [];
});

// An unsaved edit is written before another listing shows or the page closes.
watch(target, flush);
watch([target, () => id], resetWrite);
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

/** submitLink links the Workshop item typed in the dialog, unless the field is blank. */
function submitLink(): void {
  if (workshopItem.value.trim()) link(workshopItem.value);
}

/** openListing shows one listing of a mod and leaves the compare. */
function openListing(nextMod: string, key: string): void {
  selectedMod.value = nextMod;
  listingKey.value = key;
  comparing.value = false;
  picked.value = "";
}

/** openOlder opens the dialog that adds an older listing. */
function openOlder(mode: "create" | "import"): void {
  olderMode.value = mode;
  older.value = true;
}

/** toModItem returns a mod's row in the picker. An external mod's row is dimmed. */
function toModItem(workspaceMod: Mod) {
  return {
    label: workspaceMod.descriptor.name,
    description: describeMod(workspaceMod),
    class: workspaceMod.external ? `${TWO_LINES} opacity-60` : TWO_LINES,
    slot: "mod" as const,
    mod: workspaceMod,
    active: workspaceMod.id === modId.value,
    onSelect: () => openListing(workspaceMod.id, ""),
  };
}

/** describeMod returns a mod's second line in the picker: its kind if external, else its version and state on Steam. */
function describeMod(workspaceMod: Mod): string {
  if (workspaceMod.external) return workspaceMod.workshop ? "Steam's copy" : "External mod";
  const asked = queryCache.getQueryData(comparisonQuery({ modId: workspaceMod.id, key: "" }).key);
  return [workspaceMod.descriptor.version, asked && STATUSES[asked.status]?.label].filter(Boolean).join(" · ");
}

/** describeListing returns a listing's second line in the picker. */
function describeListing(modListing: Listing): string {
  const where = modListing.source ? `from ${modListing.source}` : "from the mod's folder";
  const item = modListing.workshopId ? `item ${modListing.workshopId}` : "not on the Workshop";
  return `For game ${modListing.supportedVersion} · ${item} · ${where}`;
}

/** keepFocus stops a closing menu from taking the focus back from a dialog it opened. */
function keepFocus(event: Event): void {
  event.preventDefault();
}

/** openCompare shows the compare with Steam at one difference. */
function openCompare(differenceId: string): void {
  picked.value = differenceId;
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
            <template #mod-trailing="{ item: row }">
              <UIcon
                v-if="row.mod?.external"
                :name="row.mod.workshop ? 'i-lucide-lock' : 'i-lucide-package'"
                class="size-4 text-dimmed"
              />
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
          <UBadge
            v-if="badge"
            :label="badge.label"
            :color="badge.color"
            :icon="badge.icon"
            variant="subtle"
            class="shrink-0"
          />
          <span class="grow" />
          <UDropdownMenu
            v-if="!current.external"
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

        <div class="relative flex min-h-0 grow flex-col">
          <div class="flex min-h-0 grow flex-col" :class="notice && BLURRED" :inert="!!notice">
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
                class="flex max-w-300 items-center gap-2.5 rounded-lg border border-default bg-muted px-3 py-2.5 text-13"
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
              <div class="flex max-w-300 flex-col gap-4.5">
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
          </div>
          <div v-if="notice" class="absolute inset-0 flex items-center justify-center overflow-y-auto p-6">
            <UCard :ui="{ root: 'w-120 max-w-full shadow-lg', body: 'flex flex-col gap-3' }">
              <template #header>
                <h2 class="flex items-center gap-2.5 text-16">
                  <UIcon name="i-lucide-package" class="size-4.5 text-dimmed" />{{ notice.title }}
                </h2>
              </template>
              <p class="text-13 text-toned">{{ notice.why }}</p>
              <p class="text-13 text-toned">{{ notice.how }}</p>
              <p v-if="state?.game?.id === 'ck3'" class="text-12 text-dimmed italic">
                In CK3, also delete the <code class="font-mono">remote_file_id</code> line from its
                <code class="font-mono">descriptor.mod</code> file.
              </p>
              <UButton
                v-if="notice.button"
                :label="notice.button"
                icon="i-lucide-package"
                class="self-start"
                @click="makeExternal()"
              />
              <p class="border-t border-default pt-3 text-12 text-dimmed">
                Pick one of your mods in the menu above to publish it.
              </p>
            </UCard>
          </div>
        </div>
      </section>

      <div v-if="draft && !current.external" class="h-full shrink-0" :class="notice && BLURRED" :inert="!!notice">
        <ReleaseRail
          :mod-id="modId"
          :listing-key="listingKey"
          :listing="draft"
          :comparing="showCompare"
          :picked="shown"
          @update:listing="edit"
          @compare="openCompare"
          @link="linking = true"
          @review="filesOpen = true"
        />
      </div>
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
    <UploadFiles v-if="current" v-model:open="filesOpen" :mod="current" :listing-key="listingKey" />
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
