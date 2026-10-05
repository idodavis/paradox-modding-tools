<script setup lang="ts">
/**
 * Workspace Release: listing fields, Workshop media, publish, Markdown description.
 */
import { computed, ref, shallowRef, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useClipboard } from "@vueuse/core";
import { useMutation, useQuery } from "@pinia/colada";
import { Browser } from "@wailsio/runtime";
import type { EditorCustomHandlers, EditorToolbarItem, TabsItem } from "@nuxt/ui";
import type { Editor as TiptapEditor } from "@tiptap/vue-3";
import UInputTags from "@nuxt/ui/components/InputTags.vue";
import RawEditor from "@nuxt/ui/components/Editor.vue";
import UEditorToolbar from "@nuxt/ui/components/EditorToolbar.vue";
import {
  LoadListing,
  SaveListing,
  Convert,
  DraftChangelog,
  PublishSteam,
  BumpVersion,
  CopyThumbnail,
  AddWorkshopPreview,
  RemoveWorkshopPreview,
  AddWorkshopVideo,
  RemoveWorkshopVideo,
} from "@services/releaseservice";
import type { Listing, PublishResult } from "@services/models";
import type { Preview } from "@services/internal/steam";
import { ReadFileBase64 } from "@services/fileservice";
import { useWorkspaceStore } from "../stores/workspace";
import { imageMime, releaseKeys } from "../queries";
import ReleaseModRail from "../components/release/ReleaseModRail.vue";
import WorkshopMediaCard, { type MediaSlide } from "../components/release/WorkshopMediaCard.vue";
import { pickFile } from "../lib/nativeDialog";

/** Package Editor.vue.d.ts pulls `#build/ui/editor` and types as `{}`. */
const UEditor = RawEditor as unknown as new () => {
  $props: Record<string, unknown>;
  $slots: { default: (props: { editor: TiptapEditor }) => unknown };
};

type DeployTarget = "steam" | "pdx";
type ReleaseTab = "listing" | "publish";

const route = useRoute();
const router = useRouter();
const ws = useWorkspaceStore();
const toast = useToast();
const { copy } = useClipboard();
const { id: workspaceId } = defineProps<{ id: string }>();
const liveMods = computed(() => ws.workspaceMods.filter((m) => !m.isBroken && m.path));
const selectedId = shallowRef("");
const listing = ref<Listing | null>(null);
const notes = ref<string[]>([]);
const deploy = shallowRef<DeployTarget>("steam");
const publishResult = ref<PublishResult | null>(null);
const copied = shallowRef<"" | "md" | "bb">("");
const listingThumbUrl = shallowRef("");
const thumbNonce = shallowRef(0);
const previewUrls = ref<Record<string, string>>({});
const imgPopupOpen = shallowRef(false);
const imgPopupUrl = shallowRef("");
const imgPopupEditor = shallowRef<TiptapEditor | null>(null);

const tabItems: TabsItem[] = [
  { label: "Listing", value: "listing", icon: "i-lucide-file-text" },
  { label: "Publish", value: "publish", icon: "i-lucide-cloud-upload" },
];

/** Route query `tab`, default Listing. */
const tab = computed({
  get(): string | number {
    return parseTab(route.query.tab);
  },
  set(next: string | number) {
    void router.replace({ query: { ...route.query, tab: parseTab(next) } });
  },
});

function parseTab(raw: unknown): ReleaseTab {
  const s = String(raw ?? "");
  switch (s) {
    case "listing":
    case "publish":
      return s;
    default:
      return "listing";
  }
}

const imgPopupPreview = computed(() => imgPopupUrl.value.trim().startsWith("https://"));

watch(
  liveMods,
  (mods) => {
    if (!mods.length) {
      selectedId.value = "";
      return;
    }
    if (!mods.some((m) => m.id === selectedId.value)) {
      selectedId.value = mods[0]!.id;
    }
  },
  { immediate: true },
);

const {
  data: loaded,
  error: loadError,
  isPending,
  refetch,
} = useQuery({
  key: () => releaseKeys.listing(workspaceId, selectedId.value),
  query: () => LoadListing(workspaceId, selectedId.value),
  enabled: () => Boolean(workspaceId && selectedId.value),
});

watch(loaded, async (next) => {
  listing.value = next ? asListing(next) : null;
  notes.value = [];
  publishResult.value = null;
  copied.value = "";
  const cur = listing.value;
  if (cur && !cur.readmeMd.trim() && cur.readmeBbcode.trim()) {
    const out = await Convert(cur.readmeBbcode, "markdown");
    cur.readmeMd = out.text;
    notes.value = out.notes ?? [];
  }
});

const isCk3 = computed(() => listing.value?.gameId === "ck3");

// the button's label comes from the BumpVersion RPC its click calls, so it names what it writes
const patchNext = ref("");
const minorNext = ref("");
watch(
  () => listing.value?.version ?? "",
  async (v) => {
    const [patch, minor] = await Promise.all([
      BumpVersion(v, "patch"),
      BumpVersion(v, "minor"),
    ]);
    patchNext.value = patch;
    minorNext.value = minor;
  },
  { immediate: true },
);

const mediaSlides = computed((): MediaSlide[] =>
  (listing.value?.previews ?? []).map((p) => previewSlide(p, previewUrls.value)),
);

watch([() => listing.value?.thumbnailAbs ?? "", thumbNonce], async ([abs]) => {
  const prev = listingThumbUrl.value;
  listingThumbUrl.value = "";
  if (prev) URL.revokeObjectURL(prev);
  if (!abs) return;
  const url = await blobUrl(abs);
  if (url) listingThumbUrl.value = url;
});

watch(
  () => listing.value?.previews,
  async (previews) => {
    const prev = previewUrls.value;
    const next: Record<string, string> = {};
    for (const p of previews ?? []) {
      if (p.kind !== "image" || !p.abs) continue;
      const url = await blobUrl(p.abs);
      if (url) next[p.abs] = url;
    }
    previewUrls.value = next;
    revokeAll(prev);
  },
  { deep: true },
);

/** Markdown → Workshop BBCode for save, Steam, and launcher paste. */
async function deriveBb(): Promise<void> {
  const cur = listing.value;
  if (!cur) return;
  const out = await Convert(cur.readmeMd ?? "", "bbcode");
  cur.readmeBbcode = out.text;
  notes.value = out.notes ?? [];
}

const { mutateAsync: saveMut, isLoading: saving } = useMutation({
  mutation: async () => {
    if (!listing.value) throw new Error("no listing");
    await deriveBb();
    return SaveListing(listing.value);
  },
  onSuccess: (next) => {
    listing.value = next ? asListing(next) : listing.value;
    toast.add({ title: "Listing saved", color: "success" });
    void refetch();
  },
});

const { mutateAsync: draftMut } = useMutation({
  mutation: () => DraftChangelog(workspaceId, selectedId.value, listing.value?.version ?? ""),
  onSuccess: (text) => {
    if (listing.value) listing.value.changeNote = text;
  },
});

const {
  mutateAsync: publishMut,
  isLoading: publishing,
  error: publishError,
} = useMutation({
  mutation: async () => {
    if (!listing.value) throw new Error("no listing");
    await deriveBb();
    return PublishSteam(listing.value);
  },
  onSuccess: (result) => {
    publishResult.value = result;
    if (result?.listing) listing.value = asListing(result.listing);
    toast.add({ title: "Published to Steam", color: "success" });
    void refetch();
  },
});

const { mutateAsync: bumpMut } = useMutation({
  mutation: (kind: string) => BumpVersion(listing.value?.version ?? "", kind),
  onSuccess: (next) => {
    if (listing.value) listing.value.version = next;
  },
});

const { mutateAsync: thumbMut } = useMutation({
  mutation: (src: string) => CopyThumbnail(workspaceId, selectedId.value, src),
  onSuccess: () => {
    toast.add({ title: "Thumbnail copied", color: "success" });
    thumbNonce.value++;
    void refetch();
  },
});

/** Pick a png/jpg and copy it onto the game thumbnail path. */
async function browseThumb(): Promise<void> {
  const path = await pickFile("Thumbnail", "*.png; *.jpg; *.jpeg");
  if (path) await thumbMut(path);
}

/** Native file picker → workshop/previews (not the description). */
async function addGalleryImage(): Promise<void> {
  const path = await pickFile("Workshop image", "*.png; *.jpg; *.jpeg; *.gif; *.webp");
  if (!path) return;
  try {
    const next = await AddWorkshopPreview(workspaceId, selectedId.value, path);
    if (next) listing.value = asListing(next);
    toast.add({
      title: "Added to Workshop media",
      description: "Steam extras, not the description.",
      color: "success",
    });
  } catch (e) {
    toast.add({ title: errMsg(e), color: "error" });
  }
}

/** Parse a YouTube URL/id into workshop/videos.txt. */
async function addGalleryVideo(url: string): Promise<void> {
  try {
    const next = await AddWorkshopVideo(workspaceId, selectedId.value, url);
    if (next) listing.value = asListing(next);
  } catch (e) {
    toast.add({ title: errMsg(e), color: "error" });
  }
}

/** Remove the active carousel extra. */
async function removeMedia(item: MediaSlide): Promise<void> {
  try {
    const next =
      item.kind === "youtube"
        ? await RemoveWorkshopVideo(workspaceId, selectedId.value, item.id)
        : await RemoveWorkshopPreview(workspaceId, selectedId.value, item.rel);
    if (next) listing.value = asListing(next);
  } catch (e) {
    toast.add({ title: errMsg(e), color: "error" });
  }
}

const descHandlers = {
  workshopImage: {
    canExecute: () => Boolean(selectedId.value),
    execute: (editor: TiptapEditor) => {
      imgPopupEditor.value = editor;
      imgPopupUrl.value = "";
      imgPopupOpen.value = true;
    },
    isActive: () => false,
  },
} satisfies EditorCustomHandlers;

const descToolbar: EditorToolbarItem<typeof descHandlers>[][] = [
  [
    { kind: "heading", level: 1, icon: "i-lucide-heading-1", tooltip: { text: "Heading 1" } },
    { kind: "heading", level: 2, icon: "i-lucide-heading-2", tooltip: { text: "Heading 2" } },
    { kind: "heading", level: 3, icon: "i-lucide-heading-3", tooltip: { text: "Heading 3" } },
  ],
  [
    { kind: "mark", mark: "bold", icon: "i-lucide-bold", tooltip: { text: "Bold" } },
    { kind: "mark", mark: "italic", icon: "i-lucide-italic", tooltip: { text: "Italic" } },
    { kind: "mark", mark: "strike", icon: "i-lucide-strikethrough", tooltip: { text: "Strike" } },
    { kind: "mark", mark: "code", icon: "i-lucide-code", tooltip: { text: "Code" } },
  ],
  [
    { kind: "bulletList", icon: "i-lucide-list", tooltip: { text: "List" } },
    { kind: "orderedList", icon: "i-lucide-list-ordered", tooltip: { text: "Numbered" } },
    { kind: "blockquote", icon: "i-lucide-text-quote", tooltip: { text: "Quote" } },
    { kind: "codeBlock", icon: "i-lucide-square-code", tooltip: { text: "Code block" } },
    { kind: "horizontalRule", icon: "i-lucide-separator-horizontal", tooltip: { text: "Rule" } },
  ],
  [
    { kind: "link", icon: "i-lucide-link", tooltip: { text: "Link" } },
    {
      kind: "workshopImage",
      icon: "i-lucide-image",
      tooltip: { text: "Insert image (https)" },
    },
  ],
];

/** Insert a validated https image from the toolbar popup. */
function insertImgFromPopup(): void {
  const editor = imgPopupEditor.value;
  const src = imgPopupUrl.value.trim();
  if (!editor) return;
  if (!src.startsWith("https://")) {
    toast.add({ title: "https URL required", color: "error" });
    return;
  }
  editor.chain().focus().setImage({ src }).run();
  imgPopupOpen.value = false;
  imgPopupUrl.value = "";
}

/** Drop a deleted-or-wrong Workshop id so the next publish creates a new item. */
function unlinkWorkshop(): void {
  if (!listing.value) return;
  listing.value.remoteFileId = "";
  toast.add({
    title: "Workshop id cleared",
    description: "Save or publish to write the descriptor. Next publish creates a new private item.",
    color: "neutral",
  });
}

/** Copy Markdown as written, or convert to BBCode for the launcher. */
async function copyKind(kind: "md" | "bb"): Promise<void> {
  const cur = listing.value;
  if (!cur) return;
  if (kind === "md") {
    void copy(cur.readmeMd ?? "");
    copied.value = "md";
    return;
  }
  await deriveBb();
  void copy(cur.readmeBbcode ?? "");
  copied.value = "bb";
}

/** Fill nullable listing strings so the editor always has text. */
function asListing(next: Listing): Listing {
  return {
    ...next,
    tags: next.tags ?? [],
    previews: next.previews ?? [],
    readmeMd: next.readmeMd ?? "",
    readmeBbcode: next.readmeBbcode ?? "",
    changeNote: next.changeNote ?? "",
    thumbnailAbs: next.thumbnailAbs ?? "",
    descMdRel: next.descMdRel || "mod-description.md",
    descBbRel: next.descBbRel || "mod-description.bbcode",
    workshopIgnore: next.workshopIgnore ?? "",
    workshopUrl: next.workshopUrl ?? "",
  };
}

const settingsModsTo = computed(() => ({
  name: "workspace-settings" as const,
  params: { id: workspaceId },
  query: { section: "mods" },
}));

const ignoreSummary = computed(() => {
  const custom = (listing.value?.workshopIgnore ?? "").trim();
  const base =
    "Never uploaded: dotfiles (except .metadata/), .gitignore, and patterns from .gitignore in the mod folder.";
  if (!custom) return base;
  return `${base} Extra patterns from Settings:\n${custom}`;
});

const error = computed(() => loadError.value?.message ?? publishError.value?.message ?? "");

/** ReadFileBase64 → blob URL (same as workspace thumbs; never file://). */
async function blobUrl(abs: string): Promise<string> {
  try {
    const file = await ReadFileBase64(abs);
    if (!file?.exists || !file.b64) return "";
    const bin = Uint8Array.from(atob(file.b64), (c) => c.charCodeAt(0));
    return URL.createObjectURL(new Blob([bin], { type: imageMime(abs) }));
  } catch {
    return "";
  }
}

function revokeAll(urls: Record<string, string>): void {
  for (const u of Object.values(urls)) URL.revokeObjectURL(u);
}

function previewSlide(p: Preview, urls: Record<string, string>): MediaSlide {
  const kind = p.kind === "youtube" ? "youtube" : "image";
  const src = kind === "youtube" ? `https://img.youtube.com/vi/${p.id}/mqdefault.jpg` : (urls[p.abs] ?? "");
  return { src, kind, rel: p.rel ?? "", abs: p.abs ?? "", id: p.id ?? "" };
}

function errMsg(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}
</script>

<template>
  <div class="flex h-full min-h-0 flex-col overflow-hidden">
    <div class="flex min-h-0 flex-1 overflow-hidden">
      <ReleaseModRail
        :mods="liveMods"
        :selected-id="selectedId"
        @select="selectedId = $event"
      />
      <div class="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
        <UTabs
          v-model="tab"
          :items="tabItems"
          :content="false"
          color="secondary"
          variant="pill"
          size="md"
          class="shrink-0 px-3 pt-2"
        />
        <div class="min-h-0 flex-1 overflow-auto p-3">
          <UAlert v-if="error" color="error" variant="subtle" class="mb-3" :description="error" />
          <p v-if="isPending" class="text-sm text-muted">Loading listing…</p>
          <div v-else-if="listing" class="flex flex-col gap-3">
            <div v-show="tab === 'listing'" class="flex flex-col gap-3">
              <div class="grid items-stretch gap-3 lg:grid-cols-2">
                <UCard class="h-full" :ui="{ root: 'flex h-full flex-col', body: 'flex-1' }">
                  <template #header>
                    <span class="font-semibold">Listing</span>
                  </template>
                  <div class="flex flex-col gap-3">
                    <div class="flex items-end justify-between gap-2">
                      <UFormField label="Name" class="min-w-0 flex-1">
                        <UInput v-model="listing.name" />
                      </UFormField>
                      <UButton
                        label="Save listing"
                        icon="i-lucide-save"
                        size="sm"
                        :loading="saving"
                        @click="saveMut()"
                      />
                    </div>
                    <div class="grid gap-3 sm:grid-cols-2">
                      <UFormField label="Version" :description="`patch → ${patchNext} · minor → ${minorNext}`">
                        <UFieldGroup class="w-full">
                          <UInput v-model="listing.version" class="w-full" />
                          <UButton
                            :label="`patch → ${patchNext}`"
                            variant="outline"
                            size="sm"
                            @click="bumpMut('patch')"
                          />
                          <UButton
                            :label="`minor → ${minorNext}`"
                            variant="outline"
                            size="sm"
                            @click="bumpMut('minor')"
                          />
                        </UFieldGroup>
                      </UFormField>
                      <UFormField label="Supported version" description="Wildcard ok, e.g. 1.19.*">
                        <UInput v-model="listing.supportedVersion" />
                      </UFormField>
                    </div>
                    <UFormField label="Tags">
                      <UInputTags
                        :model-value="listing.tags ?? []"
                        placeholder="Add tag"
                        @update:model-value="listing.tags = $event"
                      />
                    </UFormField>
                    <UFormField label="Thumbnail" :help="listing.thumbnailRel || 'None'">
                      <div class="flex items-center gap-3">
                        <img
                          v-if="listingThumbUrl"
                          :key="listingThumbUrl"
                          :src="listingThumbUrl"
                          alt=""
                          class="size-24 shrink-0 rounded-sm object-cover"
                        />
                        <UButton
                          label="Choose image"
                          icon="i-lucide-image"
                          variant="outline"
                          size="sm"
                          @click="browseThumb"
                        />
                      </div>
                    </UFormField>
                    <UFormField label="Description files" :description="`${listing.descMdRel} · ${listing.descBbRel}`">
                      <UButton
                        label="Open in Settings"
                        icon="i-lucide-settings"
                        variant="outline"
                        size="sm"
                        :to="settingsModsTo"
                      />
                    </UFormField>
                  </div>
                </UCard>
                <WorkshopMediaCard
                  :items="mediaSlides"
                  @add-image="addGalleryImage"
                  @add-youtube="addGalleryVideo"
                  @remove="removeMedia"
                />
              </div>
              <UCard :ui="{ body: 'p-0 sm:p-0' }">
                <template #header>
                  <span class="font-semibold">Description</span>
                </template>
                <UAlert
                  v-if="notes.length"
                  color="warning"
                  variant="subtle"
                  class="mx-4 mt-3"
                  :description="notes.join(' ')"
                />
                <div class="flex h-[min(36rem,70vh)] min-h-0 flex-col">
                  <UEditor
                    :key="selectedId"
                    v-slot="{ editor }"
                    v-model="listing.readmeMd"
                    content-type="markdown"
                    :mention="false"
                    :starter-kit="{ heading: { levels: [1, 2, 3] }, underline: false }"
                    :handlers="descHandlers"
                    placeholder="Mod description…"
                    class="flex min-h-0 flex-1 flex-col"
                    :ui="{
                      content: 'min-h-0 flex-1 overflow-auto',
                      base: 'px-4 py-3',
                    }"
                  >
                    <UEditorToolbar :editor="editor" :items="descToolbar" class="border-b border-default px-2 py-1" />
                  </UEditor>
                </div>
              </UCard>
            </div>
            <div v-show="tab === 'publish'">
              <UCard>
                <template #header>
                  <div class="flex flex-wrap items-center justify-between gap-2">
                    <span class="font-semibold">Publish</span>
                    <UFieldGroup size="sm">
                      <UButton
                        label="Steam Workshop"
                        icon="i-lucide-cloud-upload"
                        :variant="deploy === 'steam' ? 'solid' : 'outline'"
                        :color="deploy === 'steam' ? 'primary' : 'neutral'"
                        @click="deploy = 'steam'"
                      />
                      <UButton
                        label="Paradox Mods"
                        icon="i-lucide-package"
                        :variant="deploy === 'pdx' ? 'solid' : 'outline'"
                        :color="deploy === 'pdx' ? 'primary' : 'neutral'"
                        @click="deploy = 'pdx'"
                      />
                    </UFieldGroup>
                  </div>
                </template>
                <div v-if="deploy === 'steam'" class="flex flex-col gap-3">
                  <UAlert
                    color="neutral"
                    variant="subtle"
                    description="Steam client must be running as the Workshop owner. New items stay private until you set them Public on Workshop. Do not run a local copy and a Workshop subscribe of the same mod together."
                  />
                  <UFormField label="Change note" help="Saved as changelog/<version>.bbcode and sent to Steam">
                    <UTextarea v-model="listing.changeNote" :rows="5" class="min-h-24 w-full font-mono text-sm" />
                  </UFormField>
                  <UFormField
                    label="Workshop item"
                    description="Written to descriptor remote_file_id after the first successful publish. Later publishes update that item. If you deleted it on Workshop, unlink and publish to create a new private item."
                  >
                    <UFieldGroup class="w-full">
                      <UInput
                        v-model="listing.remoteFileId"
                        placeholder="empty = create a new private item"
                        class="w-full font-mono text-sm"
                      />
                      <UButton
                        v-if="listing.remoteFileId"
                        label="Unlink"
                        variant="outline"
                        color="neutral"
                        size="sm"
                        @click="unlinkWorkshop"
                      />
                    </UFieldGroup>
                  </UFormField>
                  <UButton
                    v-if="listing.workshopUrl"
                    label="Open on Steam Workshop"
                    icon="i-lucide-external-link"
                    variant="soft"
                    size="sm"
                    @click="Browser.OpenURL(listing.workshopUrl)"
                  />
                  <UFormField label="Upload exclusions" :description="ignoreSummary">
                    <UButton
                      label="Open in Settings"
                      icon="i-lucide-settings"
                      variant="outline"
                      size="sm"
                      :to="settingsModsTo"
                    />
                  </UFormField>
                  <div class="flex justify-between">
                    <UButton
                      label="Redraft"
                      variant="outline"
                      size="sm"
                      icon="i-lucide-refresh-cw"
                      @click="draftMut()"
                    />
                    <UButton
                      label="Publish to Steam"
                      icon="i-lucide-upload"
                      color="secondary"
                      :loading="publishing"
                      :disabled="!listing.name"
                      @click="publishMut()"
                    />
                  </div>
                  <UAlert
                    v-if="publishError?.message"
                    color="error"
                    variant="subtle"
                    :description="publishError.message"
                  />
                  <UAlert
                    v-if="publishResult?.publishedFileId"
                    color="success"
                    variant="subtle"
                    :title="`Published ${publishResult.publishedFileId}`"
                    :description="
                      publishResult.needsLegalAgreement
                        ? 'Steam needs the Workshop legal agreement accepted.'
                        : 'Item is private on Workshop until you change visibility.'
                    "
                  />
                  <UButton
                    v-if="publishResult?.legalUrl"
                    label="Open Workshop legal agreement"
                    variant="outline"
                    size="sm"
                    @click="Browser.OpenURL(publishResult.legalUrl)"
                  />
                </div>
                <div v-else class="space-y-3 text-sm">
                  <UAlert
                    color="neutral"
                    variant="subtle"
                    description="Paradox Mods has no API. Paste into the Paradox Launcher yourself. The site often strips line breaks and BBCode. It does not take Markdown, and the launcher does not convert BBCode to Markdown."
                  />
                  <div class="flex flex-wrap gap-2">
                    <UButton
                      :label="copied === 'bb' ? 'Copied BBCode' : 'Copy BBCode'"
                      icon="i-lucide-clipboard"
                      @click="copyKind('bb')"
                    />
                    <UButton
                      :label="copied === 'md' ? 'Copied Markdown' : 'Copy Markdown'"
                      icon="i-lucide-clipboard"
                      color="neutral"
                      variant="outline"
                      @click="copyKind('md')"
                    />
                  </div>
                  <p class="text-xs text-muted">Copy BBCode to paste into the launcher.</p>
                  <ol class="list-decimal space-y-1.5 ps-5 text-muted">
                    <li>Copy BBCode. Open Paradox Launcher yourself.</li>
                    <li v-if="isCk3">
                      Folder not under Documents/Paradox Interactive/Crusader Kings III/mod/: CK3 needs a sibling
                      Documents/…/mod/&lt;name&gt;.mod with path=. PMT does not write that pointer.
                    </li>
                    <li v-else>
                      Folder not under Documents/Paradox Interactive/&lt;Game&gt;/mod/: use Add more mods or move the
                      folder. PMT does not write a pointer file.
                    </li>
                    <li>Left: Mod library / installed mods. Top right: Upload Mod.</li>
                    <li>Choose this mod. Choose Paradox Mods (does not sync to Steam).</li>
                    <li>
                      Paste BBCode. If updating, confirm the launcher fetched the live description, then replace if you
                      want.
                    </li>
                    <li>
                      Drag thumbnail under the description (~900×500, png/jpg, 1MB). The on-disk Steam thumb is square
                      and may not match this crop.
                    </li>
                    <li>Upload. Wait for verification; the site often strips line breaks/BBCode.</li>
                  </ol>
                </div>
              </UCard>
            </div>
          </div>
          <p v-else class="text-sm text-muted">Select a mod to edit its listing.</p>
        </div>
      </div>
    </div>
    <UModal
      :open="publishing"
      title="Publishing to Steam"
      description="Steam client must be running. This can take several minutes."
      :dismissible="false"
    />
    <UModal
      v-model:open="imgPopupOpen"
      title="Insert image"
      description="Description images must be https URLs. Local files belong in Workshop media."
    >
      <template #body>
        <form class="flex flex-col gap-3" @submit.prevent="insertImgFromPopup">
          <UFormField label="Image URL">
            <UInput v-model="imgPopupUrl" placeholder="https://…" />
          </UFormField>
          <img
            v-if="imgPopupPreview"
            :src="imgPopupUrl.trim()"
            alt=""
            class="max-h-48 w-full rounded-lg object-contain"
          />
        </form>
      </template>
      <template #footer>
        <UButton label="Insert" icon="i-lucide-image" :disabled="!imgPopupPreview" @click="insertImgFromPopup" />
      </template>
    </UModal>
  </div>
</template>
