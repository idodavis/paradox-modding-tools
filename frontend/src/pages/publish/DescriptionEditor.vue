<!-- The listing's description, edited as it looks on Steam or as its BBCode. -->
<script setup lang="ts">
import { computed, nextTick, shallowRef, useTemplateRef, watch } from "vue";
import { refAutoReset, until } from "@vueuse/core";
import { useMutation } from "@pinia/colada";
import { Clipboard } from "@wailsio/runtime";
import { TableKit } from "@tiptap/extension-table";
import type { EditorCustomHandlers, EditorHandler, EditorToolbarItem, TabsItem } from "@nuxt/ui";

import { plural } from "../../lib/format";
import { htmlQuery, loadQuery } from "../../queries";
import * as steam from "@services/steam/service";
import type { Comparison, Listing } from "@services/steam/models";

/** The visual view's editor. */
type Editor = Parameters<EditorHandler["execute"]>[0];
/** A chain of editor commands. */
type Chain = ReturnType<Editor["chain"]>;

/** The commands of the Table menu. */
const TABLE_COMMANDS: readonly {
  kind: string;
  label: string;
  icon: string;
  color?: "error";
  add: (chain: Chain) => Chain;
}[] = [
  {
    kind: "insertTable",
    label: "Insert a table",
    icon: "i-lucide-table",
    add: (chain) => chain.insertTable({ rows: 3, cols: 3, withHeaderRow: true }),
  },
  {
    kind: "addRowAfter",
    label: "Add a row below",
    icon: "i-lucide-between-horizontal-start",
    add: (chain) => chain.addRowAfter(),
  },
  {
    kind: "addColumnAfter",
    label: "Add a column to the right",
    icon: "i-lucide-between-vertical-start",
    add: (chain) => chain.addColumnAfter(),
  },
  {
    kind: "toggleHeaderRow",
    label: "Header row on or off",
    icon: "i-lucide-panel-top",
    add: (chain) => chain.toggleHeaderRow(),
  },
  {
    kind: "deleteRow",
    label: "Delete the row",
    icon: "i-lucide-trash-2",
    color: "error",
    add: (chain) => chain.deleteRow(),
  },
  {
    kind: "deleteColumn",
    label: "Delete the column",
    icon: "i-lucide-trash-2",
    color: "error",
    add: (chain) => chain.deleteColumn(),
  },
  {
    kind: "deleteTable",
    label: "Delete the table",
    icon: "i-lucide-trash-2",
    color: "error",
    add: (chain) => chain.deleteTable(),
  },
];
/** The editor's handlers for the table commands. */
const TABLE_HANDLERS: EditorCustomHandlers = Object.fromEntries(
  TABLE_COMMANDS.map((command) => [
    command.kind,
    {
      canExecute: (target: Editor) => command.add(target.can().chain()).run(),
      execute: (target: Editor) => command.add(target.chain().focus()),
      isActive: () => false,
    },
  ]),
);
/** Tables in the editor, without column widths, as Steam draws them. */
const EXTENSIONS = [TableKit.configure({ table: { resizable: false } })];

/** The visual view's toolbar. */
const TOOLBAR: EditorToolbarItem<typeof TABLE_HANDLERS>[][] = [
  [
    {
      icon: "i-lucide-heading",
      tooltip: { text: "Heading" },
      content: { align: "start" },
      items: ([1, 2, 3] as const).map((level) => ({
        kind: "heading" as const,
        level,
        icon: `i-lucide-heading-${level}`,
        label: `Heading ${level}`,
      })),
    },
  ],
  [
    { kind: "mark", mark: "bold", icon: "i-lucide-bold", tooltip: { text: "Bold" } },
    { kind: "mark", mark: "italic", icon: "i-lucide-italic", tooltip: { text: "Italic" } },
    { kind: "mark", mark: "underline", icon: "i-lucide-underline", tooltip: { text: "Underline" } },
    { kind: "mark", mark: "strike", icon: "i-lucide-strikethrough", tooltip: { text: "Strike" } },
  ],
  [
    { kind: "bulletList", icon: "i-lucide-list", tooltip: { text: "Bulleted list" } },
    { kind: "orderedList", icon: "i-lucide-list-ordered", tooltip: { text: "Numbered list" } },
  ],
  [
    { kind: "blockquote", icon: "i-lucide-text-quote", tooltip: { text: "Quote" } },
    { kind: "codeBlock", icon: "i-lucide-square-code", tooltip: { text: "Code block" } },
    { kind: "horizontalRule", icon: "i-lucide-separator-horizontal", tooltip: { text: "Rule" } },
  ],
  [
    {
      icon: "i-lucide-table",
      tooltip: { text: "Table" },
      content: { align: "start" },
      items: TABLE_COMMANDS.map(({ kind, label, icon, color }) => ({ kind, label, icon, color })),
    },
  ],
  [{ slot: "link" as const }, { slot: "image" as const }],
];
/** What an empty description shows. */
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
/** The toolbar buttons that ask for an address. */
const ADDRESSES = [
  {
    slot: "link",
    label: "Link",
    icon: "i-lucide-link",
    run: (target: Editor, href: string) => target.chain().focus().setLink({ href }).run(),
  },
  {
    slot: "image",
    label: "Image",
    icon: "i-lucide-image",
    run: (target: Editor, src: string) => target.chain().focus().setImage({ src }).run(),
  },
];

const {
  modId,
  listingKey,
  comparison = undefined,
} = defineProps<{ modId: string; listingKey: string; comparison?: Comparison }>();
const listing = defineModel<Listing>("listing", { required: true });

const view = shallowRef("visual");
// whether the user picked the view, which then never switches on its own
const chosen = shallowRef(false);
// whether the visual editor would change the description
const lossy = shallowRef(false);
// the description's HTML, undefined until it is drawn
const html = shallowRef<string>();
// counts the drawings, so one that ends late is dropped
let drawing = 0;
const address = shallowRef("");
const addressing = shallowRef("");
const copied = refAutoReset(false, 2000);
const visual = useTemplateRef<{ editor?: Editor }>("visual");
const editor = computed(() => visual.value?.editor);
// where the toolbar shows, in the card's header
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

const { mutate: convert } = useMutation({
  mutation: (text: string) => steam.ToBBCode(text),
  // an out-of-date conversion, or one that changes nothing, writes nothing
  onSuccess: (bbcode, text) => {
    if (text === html.value && bbcode !== description.value) description.value = bbcode;
  },
});

// another listing starts over in the visual view
watch([() => modId, () => listingKey], () => {
  html.value = undefined;
  lossy.value = false;
  chosen.value = false;
  view.value = "visual";
});
// the description is drawn again when the listing or the view changes, never while the user types
watch([() => modId, () => listingKey, view], () => showDescription(description.value), { immediate: true });

/**
 * showDescription draws BBCode in the visual editor and notes whether the editor would change it. Unless the user
 * picked the view, such a description shows as BBCode.
 */
async function showDescription(bbcode: string): Promise<void> {
  if (view.value !== "visual") return;
  const turn = ++drawing;
  // an open editor would take the new text as an edit
  html.value = undefined;
  const drawn = await loadQuery(htmlQuery(bbcode));
  html.value = drawn;
  await nextTick();
  const kept = await until(editor).toBeTruthy({ timeout: 2000 });
  if (!kept) return;
  const back = await loadQuery(htmlQuery(await steam.ToBBCode(kept.getHTML())));
  if (turn !== drawing) return;
  lossy.value = back !== drawn;
  if (lossy.value && !chosen.value) view.value = "bbcode";
}

/** choose shows the view the user picked and keeps it. */
function choose(choice: string | number): void {
  chosen.value = true;
  view.value = `${choice}`;
}

/** change keeps the editor's text and writes its BBCode to the listing. The description is then the editor's own. */
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

/** copyDescription copies the description's BBCode, for the Paradox Launcher. */
async function copyDescription(): Promise<void> {
  await Clipboard.SetText(description.value);
  copied.value = true;
}

/** insert runs an address button's command with the typed address and closes its popover. */
function insert(target: Editor, run: (target: Editor, address: string) => unknown): void {
  run(target, address.value.trim());
  address.value = "";
  addressing.value = "";
}
</script>

<template>
  <div class="rounded-lg border border-default bg-(--bg-0)">
    <div
      class="sticky top-0 z-10 flex h-9 items-center gap-0.5 rounded-t-lg border-b border-default bg-elevated px-1.5"
    >
      <span ref="toolbarSpot" class="contents" />
      <UButton
        v-if="view === 'bbcode' && description"
        :label="copied ? 'Copied' : 'Copy for Paradox Mods'"
        :icon="copied ? 'i-lucide-check' : 'i-lucide-copy'"
        size="xs"
        variant="ghost"
        @click="copyDescription"
      />
      <span class="grow" />
      <UButton
        v-if="steamText"
        label="Start from Steam's text"
        size="xs"
        variant="ghost"
        class="me-1.5"
        @click="copySteamText"
      />
      <span class="me-2 text-11 text-dimmed">{{ plural(words, "word") }}</span>
      <UTabs :model-value="view" :items="VIEWS" :content="false" size="xs" @update:model-value="choose" />
    </div>
    <p v-if="lossy" class="border-b border-default px-5.5 py-1.5 text-12 text-warning">{{ LOSSY_NOTES[view] }}</p>
    <!-- no empty line after a last heading or list, so a click rewrites nothing -->
    <UEditor
      v-if="view === 'visual' && html !== undefined"
      ref="visual"
      :model-value="html"
      content-type="html"
      :mention="false"
      :placeholder="PLACEHOLDER"
      :starter-kit="{
        heading: { levels: [1, 2, 3] },
        link: { openOnClick: false, HTMLAttributes: { target: null, rel: null } },
        trailingNode: false,
      }"
      :extensions="EXTENSIONS"
      :handlers="TABLE_HANDLERS"
      :ui="{ content: 'min-h-96 px-5.5 py-4' }"
      @update:model-value="change"
    >
      <!-- inside the editor, so it gets the table handlers; shown in the card's header -->
      <Teleport defer :to="toolbarSpot">
        <UEditorToolbar v-if="editor" :editor="editor" :items="TOOLBAR">
          <template v-for="row in ADDRESSES" :key="row.slot" #[row.slot]>
            <UPopover :open="addressing === row.slot" @update:open="addressing = $event ? row.slot : ''">
              <UTooltip :text="row.label">
                <UButton :icon="row.icon" color="neutral" variant="ghost" size="sm" square :aria-label="row.label" />
              </UTooltip>
              <template #content>
                <form class="flex gap-2 p-2.5" @submit.prevent="insert(editor, row.run)">
                  <UInput v-model="address" :placeholder="`${row.label} address`" autofocus class="w-64" />
                  <UButton type="submit" label="Add" color="primary" variant="solid" :disabled="!address.trim()" />
                </form>
              </template>
            </UPopover>
          </template>
        </UEditorToolbar>
      </Teleport>
    </UEditor>
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
