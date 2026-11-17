<!--
  The rich text editor of a listing's description and of a Markdown file's Visual view, with its toolbar in a bar the
  page holds.
-->
<script lang="ts">
import { generateJSON } from "@tiptap/core";
import Image from "@tiptap/extension-image";
import { TableKit } from "@tiptap/extension-table";
import { MarkdownManager } from "@tiptap/markdown";
import StarterKit, { type StarterKitOptions } from "@tiptap/starter-kit";

/**
 * STARTER_KIT sets the editor's own blocks and marks: three heading levels, links that a click does not follow and that
 * open no new window, and no empty line after a last heading or list, so a click rewrites nothing.
 */
const STARTER_KIT: Partial<StarterKitOptions> = {
  heading: { levels: [1, 2, 3] },
  link: { openOnClick: false, HTMLAttributes: { target: null, rel: null } },
  trailingNode: false,
};
/**
 * TABLES are the editor's tables, without column widths, as Steam draws them.
 * The agents' answers read their tables with them too.
 */
export const TABLES = [TableKit.configure({ table: { resizable: false } })];
/** The blocks and marks the editor holds, which its Markdown is written with. */
const MARKDOWN_EXTENSIONS = [StarterKit.configure(STARTER_KIT), Image, ...TABLES];
const markdown = new MarkdownManager({ extensions: MARKDOWN_EXTENSIONS });

/** toMarkdown returns HTML as the Markdown the editor writes for it. */
export function toMarkdown(html: string): string {
  return markdown.serialize(generateJSON(html, MARKDOWN_EXTENSIONS));
}
</script>

<script setup lang="ts">
import { computed, shallowRef, useTemplateRef } from "vue";
import type { EditorCustomHandlers, EditorHandler, EditorProps, EditorToolbarItem } from "@nuxt/ui";

/** The editor. */
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
/** The editor's toolbar. */
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
  toolbar,
  contentType,
  editable = true,
  placeholder = undefined,
  ui = undefined,
} = defineProps<{
  toolbar: HTMLElement | null; // the bar the toolbar is shown in; null until the page has drawn it
  contentType: "html" | "markdown";
  editable?: boolean;
  placeholder?: string;
  ui?: EditorProps["ui"];
}>();
const text = defineModel<string>({ required: true });

const address = shallowRef("");
const openSlot = shallowRef("");
const host = useTemplateRef<{ editor?: Editor }>("host");
const editor = computed(() => host.value?.editor);
defineExpose({ editor });

/** insert runs an address button's command with the typed address and closes its popover. */
function insert(target: Editor, run: (target: Editor, address: string) => unknown): void {
  run(target, address.value.trim());
  address.value = "";
  openSlot.value = "";
}
</script>

<template>
  <UEditor
    ref="host"
    v-model="text"
    :content-type="contentType"
    :editable="editable"
    :placeholder="placeholder"
    :ui="ui"
    :mention="false"
    :starter-kit="STARTER_KIT"
    :extensions="TABLES"
    :handlers="TABLE_HANDLERS"
  >
    <!-- Inside the editor, so it gets the table handlers, and shown in the page's bar. -->
    <Teleport v-if="toolbar" defer :to="toolbar">
      <UEditorToolbar v-if="editor" :editor="editor" :items="TOOLBAR">
        <template v-for="row in ADDRESSES" :key="row.slot" #[row.slot]>
          <UPopover :open="openSlot === row.slot" @update:open="openSlot = $event ? row.slot : ''">
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
</template>
