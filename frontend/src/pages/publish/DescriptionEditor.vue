<!-- The description editor: its source format, its text, and the BBCode the text becomes. -->
<script setup lang="ts">
import { computed, shallowRef } from "vue";
import { useQuery } from "@pinia/colada";
import type { EditorToolbarItem, TabsItem } from "@nuxt/ui";

import { bbcodeQuery } from "../../queries";
import type { Listing } from "@services/steam/models";

/** The sources a description is kept in. Markdown and BBCode can be picked only while it is Steam's own text. */
const SOURCES = [
  { label: "Markdown", value: "markdown" },
  { label: "BBCode", value: "bbcode" },
  { label: "Steam's own text", value: "steam" },
];
const VIEWS: TabsItem[] = [
  { label: "Edit", value: "edit" },
  { label: "BBCode", value: "bbcode" },
];
// every mark and block the BBCode conversion carries, but links and images
const TOOLBAR: EditorToolbarItem[][] = [
  [1, 2, 3].map((level) => ({
    kind: "heading",
    level,
    icon: `i-lucide-heading-${level}`,
    tooltip: { text: `Heading ${level}` },
  })),
  [
    { kind: "mark", mark: "bold", icon: "i-lucide-bold", tooltip: { text: "Bold" } },
    { kind: "mark", mark: "italic", icon: "i-lucide-italic", tooltip: { text: "Italic" } },
    { kind: "mark", mark: "strike", icon: "i-lucide-strikethrough", tooltip: { text: "Strike" } },
    { kind: "mark", mark: "code", icon: "i-lucide-code", tooltip: { text: "Code" } },
  ],
  [
    { kind: "bulletList", icon: "i-lucide-list", tooltip: { text: "Bulleted list" } },
    { kind: "orderedList", icon: "i-lucide-list-ordered", tooltip: { text: "Numbered list" } },
    { kind: "blockquote", icon: "i-lucide-text-quote", tooltip: { text: "Quote" } },
    { kind: "codeBlock", icon: "i-lucide-square-code", tooltip: { text: "Code block" } },
    { kind: "horizontalRule", icon: "i-lucide-separator-horizontal", tooltip: { text: "Rule" } },
  ],
];

const listing = defineModel<Listing>("listing", { required: true });

const view = shallowRef("edit");
const description = computed({
  get: () => listing.value.description,
  set: (text) => (listing.value = { ...listing.value, description: text }),
});
const source = computed({
  get: () => listing.value.source,
  set: (next) => (listing.value = { ...listing.value, source: next }),
});
const { data: bbcode } = useQuery(() => ({
  ...bbcodeQuery(description.value),
  enabled: source.value === "markdown" && view.value === "bbcode",
}));
</script>

<template>
  <div class="flex min-h-0 w-170 shrink-0 flex-col gap-2.5 pt-4.5 pb-5">
    <div class="flex items-center gap-2">
      <span class="text-13 font-semibold">Description</span>
      <span class="min-w-0 truncate font-mono text-11 text-dimmed">{{ listing.descriptionPath }}</span>
      <span class="grow" />
      <UTabs
        v-if="source === 'markdown'"
        :model-value="view"
        :items="VIEWS"
        :content="false"
        @update:model-value="view = `${$event}`"
      />
    </div>
    <URadioGroup v-model="source" :items="SOURCES" :disabled="source !== 'steam'" orientation="horizontal" size="sm" />
    <p v-if="source === 'steam'" class="text-12 text-dimmed">
      Steam keeps its own text: uploads leave it as it is. Edit it on the Workshop page, or pick Markdown or BBCode to
      write it here.
    </p>
    <div v-else class="flex min-h-96 grow flex-col overflow-hidden rounded-lg border border-default bg-muted">
      <UEditor
        v-if="source === 'markdown' && view === 'edit'"
        v-slot="{ editor }"
        v-model="description"
        content-type="markdown"
        :mention="false"
        :starter-kit="{ heading: { levels: [1, 2, 3] }, underline: false }"
        class="flex min-h-0 grow flex-col"
        :ui="{ content: 'min-h-0 grow overflow-auto bg-(--bg-0)' }"
      >
        <div class="flex h-9 shrink-0 items-center gap-0.5 border-b border-default bg-elevated px-2">
          <UEditorToolbar :editor="editor" :items="TOOLBAR" />
          <span class="grow" />
          <span class="truncate pe-1.5 text-11 text-dimmed">Markdown becomes BBCode on upload</span>
        </div>
      </UEditor>
      <pre
        v-else-if="source === 'markdown'"
        class="min-h-0 grow overflow-auto bg-(--bg-0) px-4.5 py-4 font-mono text-12 whitespace-pre-wrap"
        >{{ bbcode }}</pre>
      <UTextarea
        v-else
        v-model="description"
        variant="none"
        class="grow bg-(--bg-0) [&_textarea]:h-full [&_textarea]:resize-none [&_textarea]:px-4.5 [&_textarea]:py-4 [&_textarea]:font-mono [&_textarea]:text-12 md:[&_textarea]:text-12"
      />
    </div>
  </div>
</template>
