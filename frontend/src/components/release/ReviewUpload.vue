<script setup lang="ts">
/**
 * Review and upload (the PublishSteam board): where to publish, what to check before an upload (Steam's
 * screenshots and videos among them, which it can bring in; last, what it changes on Steam with the way into the
 * comparison, O-R40), the Steam item and its change note, and Upload, off until everything is saved, whose result
 * stays on screen. Paradox Mods takes the description as BBCode to paste.
 */
import { computed, shallowRef } from "vue";
import { useClipboard } from "@vueuse/core";
import { useMutation, useQuery, useQueryCache } from "@pinia/colada";
import { Browser } from "@wailsio/runtime";
import type { RadioGroupItem } from "@nuxt/ui";
import type { Listing, ListingSummary, SteamCompare } from "@services/models";
import { Convert, ImportSteamExtras, PublishSteam } from "@services/releaseservice";
import { useLiveEnabled } from "../../composables/useLiveEnabled";
import { rowsOf, sitesOf } from "../../composables/useHealth";
import { healthQuery, imageQuery, languageQuery, releaseKeys, uploadFilesQuery } from "../../queries";
import { bytes, plural } from "../../lib/format";

// saved: nothing unsaved, so an upload sends what is on screen
const { listing, summary, steam, saved } = defineProps<{ listing: Listing; summary: ListingSummary; steam?: SteamCompare | null; saved: boolean }>();
const emit = defineEmits<{ back: []; compare: [] }>();

const queryCache = useQueryCache();
const ids = computed(() => ({ workspaceId: listing.workspaceId, modId: listing.modId }));
const live = useLiveEnabled(() => listing.workspaceId);
const { data: health } = useQuery(() => healthQuery({ id: listing.workspaceId, live: live.value }));
const { data: language } = useQuery(() => languageQuery(listing.workspaceId));
const { data: upload } = useQuery(() => uploadFilesQuery(ids.value));
const { data: image } = useQuery(() => imageQuery(listing.thumbnailAbs));

const where = shallowRef("steam");
const places: RadioGroupItem[] = [
  { label: "Steam Workshop", description: "PMT uploads through your Steam client. Steam must be running and signed in.", value: "steam" },
  { label: "Paradox Mods", description: "PMT prepares the description; you upload the mod with the Paradox Launcher.", value: "pdx" },
];

/** A check before an upload: ok passes, not ok warns, no ok only informs; icon, a mark of its own. */
type Check = { ok?: boolean; text: string; link?: "health" | "exclusions" | "import"; icon?: string };
const checks = computed((): Check[] => {
  const version = listing.version;
  const dangling = rowsOf(health.value, "dangling").filter((row) =>
    sitesOf(row).some((site) => health.value?.sources?.[site.file]?.origin === listing.modId),
  ).length;
  const small = !image.value || image.value.bytes < 1048576;
  // an upload replaces Steam's extras with the mod's, or leaves them when the mod keeps none
  const mine = listing.previews?.length ?? 0;
  const only = steam?.onlyOnSteam?.length ?? 0;
  const extras: Check[] = !steam?.item
    ? []
    : !mine
      ? [{ text: `Leaves Steam's screenshots and videos as they are${only ? ` (${only} not in the workspace)` : ""}`, link: "import" }]
      : only
        ? [{ ok: false, text: `Removes ${only} of Steam's screenshots and videos the workspace lacks`, link: "import" }]
        : [{ text: `Replaces Steam's screenshots and videos with the mod's ${mine}` }];
  return [
    !steam?.item
      ? { ok: true, text: `Version ${version}, the first upload` }
      : !steam.from
        ? { text: `Version ${version}; Steam's is known once compared` }
        : { ok: version !== steam.version, text: version !== steam.version ? `Version ${version}, up from ${steam.version} on Steam` : `Version ${version}, the same as on Steam` },
    { ok: undefined, text: `Supports game ${listing.supportedVersion || "any version"}, installed ${language.value?.gameVersion ?? "unknown"}` },
    image.value
      ? { ok: small, text: `Thumbnail ${image.value.width} by ${image.value.height}, ${small ? "under" : "over Steam's"} 1 MB` }
      : { ok: false, text: "No thumbnail" },
    !live.value
      ? { ok: undefined, text: "Dangling references show once the workspace is open" }
      : { ok: !dangling, text: dangling ? `${plural(dangling, "dangling reference")} in this mod` : "No dangling references in this mod", link: dangling ? "health" : undefined },
    { ok: undefined, text: upload.value ? `${upload.value.count} files, ${bytes(upload.value.bytes)} after exclusions` : "Counting the files…", link: "exclusions" },
    { ok: undefined, text: summary.descSource === "steam" ? "Leaves Steam's description as it is" : `Replaces Steam's description with ${summary.descRel}` },
    ...extras,
  ];
});

/** What an upload changes on Steam, the checks' last line, tinted, with the way into the comparison. */
const changes = computed(() => {
  if (!steam?.item) return undefined;
  if (!steam.from) return { text: "On Steam, with no copy to compare yet", tone: "bg-elevated text-dimmed" };
  const files = steam.files?.length ?? 0;
  if (!steam.changes) return { text: "Same as Steam: an upload changes nothing", tone: "bg-(--orange-soft) text-warning" };
  return { text: `${plural(steam.changes, "change")} on Steam: ${steam.changes - files} in the listing, ${plural(files, "file")}`, tone: "bg-(--blue-soft) text-info" };
});
const { mutate: importExtras, isLoading: importing } = useMutation({
  mutation: () => ImportSteamExtras(listing.workspaceId, listing.modId),
  onSettled: () => queryCache.invalidateQueries({ key: releaseKeys.mod(listing.workspaceId, listing.modId) }),
});
const ICONS: Record<string, [string, string]> = {
  true: ["i-lucide-check", "text-success"],
  false: ["i-lucide-triangle-alert", "text-warning"],
  undefined: ["i-lucide-file", "text-dimmed"],
};

const { mutate: publish, isLoading: publishing, data: published } = useMutation({
  mutation: () => PublishSteam(listing.workspaceId, listing.modId),
  onSettled: () => queryCache.invalidateQueries({ key: releaseKeys.mod(listing.workspaceId, listing.modId) }),
});
const openAgreement = () => Browser.OpenURL(published.value?.legalUrl ?? "");

const { copy, copied } = useClipboard();
// Steam's own text, as PMT's last copy of the item has it
const steamText = computed(() => steam?.listing?.find((row) => row.field === "Description")?.steam ?? "");
async function copyBBCode(): Promise<void> {
  if (summary.descSource === "steam") return void copy(steamText.value);
  void copy(summary.descSource === "markdown" ? (await Convert(listing.readmeMd, "bbcode")).text : listing.readmeBbcode);
}
</script>

<template>
  <div class="flex min-h-0 grow flex-col gap-5 px-7 pt-5">
    <URadioGroup
      :model-value="where"
      :items="places"
      variant="card"
      orientation="horizontal"
      :ui="{ fieldset: 'gap-3', item: 'flex-1', label: 'text-13', description: 'text-12 text-dimmed' }"
      @update:model-value="where = $event === 'pdx' ? 'pdx' : 'steam'"
    />

    <div class="grid grid-cols-2 gap-6">
      <div class="flex flex-col gap-2">
        <span class="text-12 font-medium text-toned">Before you upload</span>
        <div class="divide-y divide-default overflow-hidden rounded-lg border border-default bg-muted px-4 py-0.5">
          <div v-for="check in checks" :key="check.text" class="flex items-start gap-2.5 py-2.5 text-13">
            <UIcon :name="check.icon ?? ICONS[`${check.ok}`]?.[0] ?? ''" class="mt-0.75 size-3.5 shrink-0" :class="ICONS[`${check.ok}`]?.[1]" />
            <span class="flex min-w-0 flex-col gap-0.5">
              {{ check.text }}
              <ULink v-if="check.link === 'health'" :to="{ name: 'health', params: { id: listing.workspaceId }, query: { card: 'dangling' } }" class="text-12 text-secondary">
                Review in Health
              </ULink>
              <UButton
                v-else-if="check.link === 'import'"
                label="Import them from Steam"
                variant="link"
                :loading="importing"
                class="h-5 self-start p-0 text-12"
                @click="importExtras()"
              />
              <ULink
                v-else-if="check.link === 'exclusions'"
                :to="{ name: 'workspace-settings', params: { id: listing.workspaceId }, query: { section: 'mods' } }"
                class="text-12 text-secondary"
              >
                Edit upload exclusions
              </ULink>
            </span>
          </div>
          <div v-if="where === 'steam' && changes" class="-mx-4 -mb-0.5 flex items-center gap-2.5 px-4 py-2 text-13" :class="changes.tone">
            <UIcon name="i-lucide-git-compare" class="size-3.5 shrink-0" />
            <span class="min-w-0 grow text-default">{{ changes.text }}</span>
            <UButton label="Compare with Steam" size="sm" @click="emit('compare')" />
          </div>
        </div>
      </div>

      <div v-if="where === 'steam'" class="flex flex-col gap-4">
        <UFormField label="Steam item">
          <div class="flex gap-2">
            <UInput :model-value="listing.remoteFileId" readonly placeholder="A new private item" class="grow" :ui="{ base: 'font-mono text-12' }">
              <template #trailing>
                <span class="truncate text-12 text-dimmed">{{ steam?.title }}</span>
              </template>
            </UInput>
            <UButton
              v-if="listing.workshopUrl"
              label="View on Steam"
              icon="i-lucide-external-link"
              variant="ghost"
              @click="Browser.OpenURL(listing.workshopUrl)"
            />
          </div>
        </UFormField>
        <UFormField label="Change note" help="Shown in the Steam change log.">
          <template #hint>
            <UButton label="Edit" variant="link" class="p-0 text-12" @click="emit('back')" />
          </template>
          <div
            class="max-h-40 overflow-y-auto rounded-lg border border-default bg-muted px-3 py-2 text-13 whitespace-pre-wrap"
            :class="{ 'text-dimmed': !listing.changeNote }"
          >
            {{ listing.changeNote || "None: Steam's change log gets no note for this upload." }}
          </div>
        </UFormField>
      </div>
      <div v-else class="flex flex-col items-start gap-3">
        <p class="text-13 text-toned">
          Paradox Mods takes the description as BBCode: copy it, open the Paradox Launcher, choose Upload Mod, pick this mod and
          Paradox Mods, and paste it. The site often drops line breaks. Screenshots and the thumbnail are added on the site.
        </p>
        <UButton
          :label="copied ? 'Copied' : 'Copy the description as BBCode'"
          icon="i-lucide-clipboard"
          :disabled="summary.descSource === 'steam' && !steamText"
          @click="copyBBCode"
        />
      </div>
    </div>

    <UAlert
      v-if="published"
      color="success"
      variant="subtle"
      :title="`Uploaded to Steam item ${published.publishedFileId}`"
      :description="
        published.needsLegalAgreement
          ? 'Steam needs you to accept the Workshop agreement before the item shows.'
          : 'A new item stays private until you make it public on its Workshop page.'
      "
      :actions="published.legalUrl ? [{ label: 'Open the agreement', onClick: openAgreement }] : []"
    />

    <span class="grow" />
    <div class="flex items-center gap-3 border-t border-default pt-3.5 pb-4.5">
      <UButton label="Back to version and change note" icon="i-lucide-arrow-left" variant="ghost" @click="emit('back')" />
      <span class="grow" />
      <template v-if="where === 'steam'">
        <span v-if="!saved" class="text-12 text-dimmed">Save to workspace first</span>
        <span v-else class="text-12 text-dimmed">
          Uploading accepts the
          <UButton label="Steam Workshop agreement" variant="link" class="p-0 text-12" @click="Browser.OpenURL(listing.legalUrl)" />.
        </span>
        <UButton
          :label="`Upload v${listing.version} to Steam`"
          icon="i-lucide-upload"
          color="primary"
          variant="solid"
          :loading="publishing"
          :disabled="!listing.name || !saved"
          @click="publish()"
        />
      </template>
    </div>
    <UModal :open="publishing" title="Uploading to Steam" description="Steam must be running. This can take several minutes." :dismissible="false" />
  </div>
</template>
