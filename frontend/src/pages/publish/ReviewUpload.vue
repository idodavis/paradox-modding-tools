<!-- The upload review: the files sent, the rules and the DLCs, and the Publish button with its progress. -->
<script setup lang="ts">
import { computed, onScopeDispose, shallowRef } from "vue";
import { useMutation, useQuery, useQueryCache } from "@pinia/colada";
import { refAutoReset } from "@vueuse/core";
import { Browser, Clipboard, Dialogs, Events } from "@wailsio/runtime";

import { bytes, plural, steamSentences, uploadSentences } from "../../lib/format";
import {
  dlcQuery,
  headerQuery,
  keys,
  listingQuery,
  reportQuery,
  rulesQuery,
  stateQuery,
  steamStatusQuery,
} from "../../queries";
import { useWorkspaceStore } from "../../stores/workspace";
import * as app from "@services/app/service";
import * as steam from "@services/steam/service";
import type { Comparison, Listing, Progress, Upload } from "@services/steam/models";

const TARGETS = [
  {
    label: "Steam Workshop",
    description: "PMT uploads through your Steam client. Steam must be running and signed in.",
    value: "steam",
  },
  {
    label: "Paradox Mods",
    description: "PMT prepares the description; you upload the mod with the Paradox Launcher.",
    value: "paradox",
  },
];
const VISIBILITIES = [
  { label: "Private", value: "private" },
  { label: "Friends only", value: "friends" },
  { label: "Public", value: "public" },
  { label: "Unlisted", value: "unlisted" },
];
const ICONS = {
  true: { name: "i-lucide-check", color: "text-success" },
  false: { name: "i-lucide-triangle-alert", color: "text-warning" },
  undefined: { name: "i-lucide-info", color: "text-dimmed" },
};
type Check = {
  ok?: boolean;
  text: string;
  link?: { label: string; name: "health" | "workspace-settings"; section: string };
};
// Steam takes a thumbnail under one megabyte
const THUMBNAIL_BYTES = 1048576;

const { modId, upload, comparison } = defineProps<{ modId: string; upload: Upload; comparison: Comparison }>();
const emit = defineEmits<{ back: [] }>();

const toast = useToast();
const queryCache = useQueryCache();
const workspace = useWorkspaceStore();
const target = shallowRef("steam");
const tab = shallowRef("included");
const visibility = shallowRef("");
const progress = shallowRef<Progress | null>(null);
const copied = refAutoReset(false, 2000);

const { data: state } = useQuery(stateQuery);
const { data: listing } = useQuery(() => ({ ...listingQuery(modId), enabled: workspace.isOpen() }));
const { data: dlc } = useQuery(() => ({ ...dlcQuery, enabled: workspace.isOpen() }));
const { data: rules } = useQuery(rulesQuery);
const { data: report } = useQuery(() => ({
  ...reportQuery((state.value?.mods ?? []).map((mod) => mod.id)),
  enabled: workspace.isOpen(),
}));
const { data: header } = useQuery(() => ({
  ...headerQuery(listing.value?.thumbnail ?? ""),
  enabled: !!listing.value?.thumbnail,
}));
const {
  data: steamStatus,
  refetch: checkSteam,
  isLoading: checking,
} = useQuery(() => ({ ...steamStatusQuery, enabled: target.value === "steam" }));

const included = computed(() => upload.files?.map((file) => file.rel) ?? []);
const excluded = computed(() => upload.excluded ?? []);
const rows = computed(() => (tab.value === "included" ? included.value : excluded.value));
const tabs = computed(() => [
  { label: `Sent (${included.value.length})`, value: "included" },
  { label: `Left out (${excluded.value.length})`, value: "excluded" },
]);
const checks = computed<Check[]>(() => {
  const dangling = report.value?.sections?.find((section) => section.id === "dangling")?.count ?? 0;
  const thumbnail = header.value
    ? {
        ok: header.value.bytes < THUMBNAIL_BYTES,
        text: `Thumbnail ${header.value.width} by ${header.value.height}, ${bytes(header.value.bytes)}`,
      }
    : { ok: false, text: "No thumbnail" };
  return [
    {
      text: `Version ${listing.value?.version}, for game ${listing.value?.supportedVersion || "any version"}; ${state.value?.install.version} is installed`,
    },
    thumbnail,
    {
      ok: !dangling,
      text: dangling ? `${plural(dangling, "dangling reference")} in the workspace` : "No dangling references",
      link: dangling ? { label: "Review in Health", name: "health", section: "dangling" } : undefined,
    },
    {
      text: `${plural(included.value.length, "file")}, ${bytes(upload.bytes)} after the exclusions`,
      link: { label: "Edit the upload exclusions", name: "workspace-settings", section: "mods" },
    },
    {
      text:
        listing.value?.source === "steam"
          ? "Leaves Steam's description as it is"
          : "Replaces Steam's description with this mod's",
    },
    ...[...steamSentences(comparison.steam ?? null), ...uploadSentences(comparison)].map((text) => ({ text })),
  ];
});

const { mutate: write } = useMutation({ mutation: (next: Listing) => steam.WriteListing(modId, next) });
const { mutate: showFolder } = useMutation({ mutation: (path: string) => app.ShowFolder(path) });
const { mutate: publish, isLoading: publishing } = useMutation({
  mutation: () => {
    progress.value = null;
    return steam.Publish(modId, visibility.value);
  },
  onSuccess: () => toast.add({ title: "Uploaded to Steam", color: "success" }),
  onSettled: () => queryCache.invalidateQueries({ key: keys.release(modId) }),
});
const { mutate: writeZip, isLoading: zipping } = useMutation({
  mutation: async () => {
    const path = await Dialogs.SaveFile({
      Filename: `${listing.value?.title || "mod"}.zip`,
      Filters: [{ DisplayName: "Zip", Pattern: "*.zip" }],
    });
    if (path) await steam.WriteZip(modId, path);
    return path;
  },
  onSuccess: (path) =>
    path &&
    toast.add({
      title: "Zip saved",
      description: path,
      color: "success",
      actions: [{ label: "Show in the file manager", onClick: () => showFolder(path) }],
    }),
});

onScopeDispose(Events.On("steam:progress", ({ data }) => (progress.value = data)));

/** writeClipboard puts the description as BBCode on the clipboard, for pasting into the Paradox Launcher. */
async function writeClipboard(): Promise<void> {
  await Clipboard.SetText(listing.value?.bbcode ?? "");
  copied.value = true;
}
</script>

<template>
  <div class="flex min-h-0 grow flex-col gap-5 px-7 pt-5">
    <URadioGroup
      v-model="target"
      :items="TARGETS"
      variant="card"
      orientation="horizontal"
      :ui="{ fieldset: 'gap-3', item: 'flex-1', label: 'text-13', description: 'text-12 text-dimmed' }"
    />

    <div class="grid min-h-0 grow grid-cols-2 gap-6">
      <div class="flex min-h-0 flex-col gap-3">
        <span class="text-12 font-medium text-toned">Before you upload</span>
        <ul class="divide-y divide-default rounded-lg border border-default bg-muted px-4 py-0.5">
          <li v-for="check in checks" :key="check.text" class="flex items-start gap-2.5 py-2.5 text-13">
            <UIcon
              :name="ICONS[`${check.ok}`].name"
              class="mt-0.75 size-3.5 shrink-0"
              :class="ICONS[`${check.ok}`].color"
            />
            <span class="flex min-w-0 flex-col gap-0.5">
              {{ check.text }}
              <UButton
                v-if="check.link"
                :label="check.link.label"
                :to="{ name: check.link.name, params: { id: state?.id ?? '' }, query: { section: check.link.section } }"
                variant="link"
                class="h-5 self-start p-0 text-12"
              />
            </span>
          </li>
        </ul>
        <UTabs :model-value="tab" :items="tabs" :content="false" @update:model-value="tab = `${$event}`" />
        <UScrollArea
          v-slot="{ item }"
          :items="rows"
          :virtualize="{ estimateSize: 24, skipMeasurement: true }"
          class="min-h-40 grow rounded-lg border border-default bg-muted"
        >
          <div class="h-6 truncate px-3 font-mono text-12 leading-6 text-toned">{{ item }}</div>
        </UScrollArea>
        <div class="flex flex-col gap-1">
          <span class="text-12 font-medium text-toned">Every upload leaves out</span>
          <p v-for="rule in rules ?? []" :key="rule.pattern" class="text-12 text-dimmed">
            <span class="font-mono text-toned">{{ rule.pattern }}</span> {{ rule.reason }}
          </p>
        </div>
      </div>

      <div v-if="target === 'steam' && listing" class="flex flex-col gap-4">
        <UFormField label="Steam item">
          <div class="flex gap-2">
            <UInput :model-value="listing.workshopId" readonly placeholder="A new item" class="grow font-mono" />
            <UButton
              v-if="listing.workshopId"
              label="View on Steam"
              icon="i-lucide-external-link"
              variant="ghost"
              @click="Browser.OpenURL(`https://steamcommunity.com/sharedfiles/filedetails/?id=${listing.workshopId}`)"
            />
          </div>
        </UFormField>
        <UFormField label="Visibility" help="Chosen at each upload.">
          <USelect
            v-model="visibility"
            :items="VISIBILITIES"
            placeholder="Keep the item's own (private for a new item)"
            class="w-full"
          />
        </UFormField>
        <UFormField v-if="dlc?.length" label="Required DLC">
          <UCheckboxGroup
            :model-value="listing.dlc ?? []"
            :items="dlc"
            value-key="id"
            label-key="name"
            @update:model-value="write({ ...listing, dlc: $event })"
          />
        </UFormField>
        <UFormField label="Change note" help="Shown in the Steam change log.">
          <template #hint>
            <UButton label="Edit" variant="link" class="p-0 text-12" @click="emit('back')" />
          </template>
          <div
            class="max-h-40 overflow-y-auto rounded-lg border border-default bg-muted px-3 py-2 text-13 whitespace-pre-wrap"
            :class="{ 'text-dimmed': !listing.changelog }"
          >
            {{ listing.changelog || "None: Steam's change log gets no note for this upload." }}
          </div>
        </UFormField>
      </div>
      <div v-else-if="target === 'paradox'" class="flex flex-col items-start gap-3">
        <p class="text-13 text-toned">
          Paradox Mods takes the description as BBCode: copy it, open the Paradox Launcher, choose Upload Mod, pick this
          mod and Paradox Mods, and paste it. The site often drops line breaks. Screenshots and the thumbnail are added
          on the site.
        </p>
        <div class="flex flex-wrap gap-2">
          <UButton
            :label="copied ? 'Copied' : 'Copy the description as BBCode'"
            icon="i-lucide-clipboard"
            :disabled="!listing?.bbcode"
            @click="writeClipboard"
          />
          <UButton
            label="Save the mod as a zip"
            icon="i-lucide-file-archive"
            variant="outline"
            :loading="zipping"
            @click="writeZip()"
          />
        </div>
        <p class="text-12 text-dimmed">
          The zip holds what an upload sends, with the exclusions applied, for uploading on the Paradox Mods site.
        </p>
      </div>
    </div>

    <div class="flex items-center gap-3 border-t border-default pt-3.5 pb-4.5">
      <UButton
        label="Back to version and change note"
        icon="i-lucide-arrow-left"
        variant="ghost"
        @click="emit('back')"
      />
      <span class="grow" />
      <template v-if="target === 'steam'">
        <span v-if="steamStatus" class="flex min-w-0 items-center gap-1.5 text-12 text-warning">
          <UIcon name="i-lucide-triangle-alert" class="size-3.5 shrink-0" />
          <span class="truncate" :title="steamStatus">{{ steamStatus }}</span>
          <UButton label="Check again" variant="link" size="xs" class="p-0" :loading="checking" @click="checkSteam()" />
        </span>
        <span class="shrink-0 text-12 text-dimmed">
          Uploading accepts the
          <UButton
            label="Steam Workshop agreement"
            variant="link"
            class="p-0 text-12"
            @click="Browser.OpenURL('https://steamcommunity.com/sharedfiles/workshoplegalagreement')"
          />
        </span>
        <UButton
          :label="`Upload v${listing?.version} to Steam`"
          icon="i-lucide-upload"
          color="primary"
          variant="solid"
          :loading="publishing"
          :disabled="!listing?.title"
          @click="publish()"
        />
      </template>
    </div>

    <UModal
      :open="publishing"
      title="Uploading to Steam"
      description="Steam must be running. This can take several minutes."
      :dismissible="false"
    >
      <template #body>
        <div class="flex flex-col gap-2">
          <span class="text-13 text-toned">
            {{ progress?.stage ?? "starting" }}
            <template v-if="progress?.total">: {{ bytes(progress.done) }} of {{ bytes(progress.total) }}</template>
          </span>
          <UProgress :model-value="progress?.total ? (progress.done * 100) / progress.total : null" />
        </div>
      </template>
    </UModal>
  </div>
</template>
