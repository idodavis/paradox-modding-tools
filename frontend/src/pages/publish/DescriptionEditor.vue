<!-- The listing's description, edited as it looks on Steam or as its BBCode, and exported as BBCode or Markdown. -->
<script setup lang="ts">
import { computed, nextTick, shallowRef, useTemplateRef, watch } from "vue";
import { until } from "@vueuse/core";
import { useMutation } from "@pinia/colada";
import { Dialogs } from "@wailsio/runtime";
import type { DropdownMenuItem, TabsItem } from "@nuxt/ui";

import * as steam from "@services/steam/service";
import type { Comparison, Listing } from "@services/steam/models";
import { copyText, toast } from "../../lib/feedback";
import { plural } from "../../lib/format";
import { bbcodeQuery, htmlQuery, loadQuery } from "../../queries";
import { useWorkspaceStore } from "../../stores/workspace";
import RichEditor, { toMarkdown } from "../../ui/RichEditor.vue";

/** An Export is a format the description is copied or saved in, with how its text is made from the BBCode. */
type Export = { label: string; file: string; note: string; write: (bbcode: string) => Promise<string> };

const PLACEHOLDER = "Describe the mod for players. While this is empty, an upload leaves Steam's description as it is.";
/** The description's two views. */
const VIEWS: TabsItem[] = [
  { label: "Visual", value: "visual" },
  { label: "BBCode", value: "bbcode" },
];
/** The notice in each view when the visual editor would change the description. */
const LOSSY_NOTES: Record<string, string> = {
  visual: "Editing here changes some of this description's line breaks or markup to the visual editor's own.",
  bbcode:
    "The visual editor would change some of this description's line breaks or markup, so it opened as BBCode. " +
    "Choose Visual to edit it there anyway.",
};
/** The formats the description exports to. Paradox Mods takes the BBCode, pasted in the Paradox Launcher. */
const EXPORTS: Export[] = [
  {
    label: "BBCode",
    file: "description.bbcode",
    note: "Paste it in the Paradox Launcher for Paradox Mods.",
    write: (bbcode) => Promise.resolve(bbcode),
  },
  {
    label: "Markdown",
    file: "description.md",
    note: "",
    write: async (bbcode) => toMarkdown((await loadQuery(htmlQuery(bbcode))) ?? ""),
  },
];

const {
  modId,
  listingKey,
  comparison = undefined,
} = defineProps<{ modId: string; listingKey: string; comparison?: Comparison }>();
const listing = defineModel<Listing>("listing", { required: true });

const workspace = useWorkspaceStore();
const view = shallowRef("visual");
// Once the user picks the view, it never switches on its own.
const chosen = shallowRef(false);
const lossy = shallowRef(false);
const html = shallowRef<string>();
// Counts the drawings, so one that ends late is dropped.
let drawing = 0;
const visual = useTemplateRef("visual");
const editor = computed(() => visual.value?.editor);
const toolbarSpot = useTemplateRef<HTMLElement>("toolbarSpot");

const description = computed({
  get: () => listing.value.description,
  set: (text) => (listing.value = { ...listing.value, description: text }),
});
const words = computed(
  () =>
    description.value
      .replace(/\[\/?[^\]]*\]/g, " ")
      .split(/\s+/)
      .filter(Boolean).length,
);
const steamText = computed(() => (description.value ? "" : (comparison?.steam?.description ?? "")));
const exportItems = computed<DropdownMenuItem[][]>(() => [
  EXPORTS.map((format) => ({
    label: `Copy as ${format.label}`,
    icon: "i-lucide-copy",
    disabled: !description.value,
    onSelect: () => copyAs(format),
  })),
  EXPORTS.map((format) => ({
    label: `Save as ${format.label}…`,
    icon: "i-lucide-download",
    disabled: !description.value,
    onSelect: () => saveAs(format),
  })),
]);

const { mutate: convert } = useMutation({
  mutation: (text: string) => steam.ToBBCode(text),
  onSuccess: (bbcode, text) => {
    if (text === html.value && bbcode !== description.value) description.value = bbcode;
  },
});
const { mutate: copyAs } = useMutation({
  mutation: async (format: Export) => copyText(await format.write(description.value), format.label, format.note),
});
const { mutate: saveAs } = useMutation({
  mutation: async (format: Export) => {
    const target = await Dialogs.SaveFile({
      Title: `Save the description as ${format.label}`,
      Directory: workspace.listMods().find((mod) => mod.id === modId)?.dir ?? "",
      Filename: format.file,
      ButtonText: "Save",
    });
    if (!target) return;
    await steam.ExportDescription(target, await format.write(description.value));
    toast?.add({ title: `The description saved as ${format.label}`, description: target, color: "success" });
  },
});

watch([() => modId, () => listingKey], () => {
  html.value = undefined;
  lossy.value = false;
  chosen.value = false;
  view.value = "visual";
});
// The description is drawn again when the listing or the view changes, never while the user types.
watch([() => modId, () => listingKey, view], () => showDescription(description.value), { immediate: true });

/** showDescription draws BBCode in the visual editor and notes whether the editor would change it. */
async function showDescription(bbcode: string): Promise<void> {
  if (view.value !== "visual") return;
  const turn = ++drawing;

  // An open editor would take the new text as an edit.
  html.value = undefined;
  const drawn = await loadQuery(htmlQuery(bbcode));
  html.value = drawn;
  await nextTick();
  const visualEditor = await until(editor).toBeTruthy({ timeout: 2000 });
  if (!visualEditor) return;
  const roundTrip = await loadQuery(htmlQuery((await loadQuery(bbcodeQuery(visualEditor.getHTML()))) ?? ""));
  if (turn !== drawing) return;
  lossy.value = roundTrip !== drawn;
  if (lossy.value && !chosen.value) view.value = "bbcode";
}

/** choose shows the view the user picked and keeps it. */
function choose(choice: string | number): void {
  chosen.value = true;
  view.value = `${choice}`;
}

/** change keeps the visual editor's HTML and writes its BBCode to the listing. */
function change(text: string): void {
  html.value = text;
  lossy.value = false;
  convert(text);
}

/** copySteamText starts the description from Steam's text. */
function copySteamText(): void {
  const text = steamText.value;
  description.value = text;
  chosen.value = false;
  void showDescription(text);
}
</script>

<template>
  <div class="rounded-lg border border-default bg-(--bg-0)">
    <div
      class="sticky top-0 z-10 flex h-9 items-center gap-0.5 rounded-t-lg border-b border-default bg-elevated px-1.5"
    >
      <span ref="toolbarSpot" class="contents" />
      <span class="grow" />
      <UButton
        v-if="steamText"
        label="Start from Steam's text"
        size="xs"
        variant="ghost"
        class="me-1.5"
        @click="copySteamText"
      />
      <span class="me-2 shrink-0 text-11 whitespace-nowrap text-dimmed">{{ plural(words, "word") }}</span>
      <UDropdownMenu :items="exportItems" :content="{ align: 'end' }">
        <UButton label="Export" icon="i-lucide-share" size="xs" variant="ghost" class="me-1.5" />
      </UDropdownMenu>
      <UTabs :model-value="view" :items="VIEWS" :content="false" size="xs" @update:model-value="choose" />
    </div>
    <p v-if="lossy" class="border-b border-default px-5.5 py-1.5 text-12 text-warning">{{ LOSSY_NOTES[view] }}</p>
    <RichEditor
      v-if="view === 'visual' && html !== undefined"
      ref="visual"
      :model-value="html"
      content-type="html"
      :placeholder="PLACEHOLDER"
      :toolbar="toolbarSpot"
      :ui="{ content: 'min-h-96 px-5.5 py-4' }"
      @update:model-value="change"
    />
    <UTextarea
      v-else-if="view === 'bbcode'"
      v-model="description"
      autoresize
      :rows="20"
      :maxrows="0"
      variant="none"
      :placeholder="PLACEHOLDER"
      class="w-full [&_textarea]:px-5.5 [&_textarea]:py-4 [&_textarea]:font-mono [&_textarea]:text-12"
    />
  </div>
</template>
