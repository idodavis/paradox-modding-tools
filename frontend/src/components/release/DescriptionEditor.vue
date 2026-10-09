<script lang="ts">
import type { EditorToolbarItem } from "@nuxt/ui";

// every mark and block the BBCode conversion carries (owner, 2026-10-05), but links and images (owner, 2026-10-06)
export const MARKDOWN_TOOLBAR: EditorToolbarItem[][] = [
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
    { kind: "bulletList", icon: "i-lucide-list", tooltip: { text: "Bulleted list" } },
    { kind: "orderedList", icon: "i-lucide-list-ordered", tooltip: { text: "Numbered list" } },
    { kind: "blockquote", icon: "i-lucide-text-quote", tooltip: { text: "Quote" } },
    { kind: "codeBlock", icon: "i-lucide-square-code", tooltip: { text: "Code block" } },
    { kind: "horizontalRule", icon: "i-lucide-separator-horizontal", tooltip: { text: "Rule" } },
  ],
];
</script>

<script setup lang="ts">
/**
 * The listing's description (the PublishListing board's right column): its source and how to change it,
 * then Edit, Steam preview (the BBCode an upload sends, read back the way Steam shows it) and BBCode.
 */
import { computed, shallowRef } from "vue";
import { computedAsync } from "@vueuse/core";
import { useMutation, useQueryCache } from "@pinia/colada";
import type { DropdownMenuItem, TabsItem } from "@nuxt/ui";
import type { Listing, ListingSummary } from "@services/models";
import { Convert, CreateDescriptionFile, LoadListing } from "@services/releaseservice";
import { SetWorkspaceModDescription } from "@services/workspaceservice";
import { pickFile } from "../../lib/nativeDialog";
import { releaseKeys } from "../../queries";

const { listing, summary } = defineProps<{ listing: Listing; summary: ListingSummary }>();
const markdown = defineModel<string>("markdown", { required: true });
const bbcode = defineModel<string>("bbcode", { required: true });

const queryCache = useQueryCache();
const view = shallowRef<string | number>("edit");
const fromMarkdown = computed(() => summary.descSource === "markdown");
const editable = computed(() => summary.descSource !== "steam" && summary.descExists);
const views = computed<TabsItem[]>(() => [
  { label: "Edit", value: "edit" },
  { label: "Steam preview", value: "preview" },
  ...(fromMarkdown.value ? [{ label: "BBCode", value: "bbcode" }] : []),
]);

// what an upload sends, and that read back to Markdown: what Steam cannot show is gone from it
const sent = computedAsync(async () => {
  if (view.value === "edit" || !editable.value) return undefined;
  const converted = fromMarkdown.value ? await Convert(markdown.value, "bbcode") : { text: bbcode.value, notes: [] };
  return { ...converted, shown: (await Convert(converted.text, "markdown")).text };
});

// the editor takes no placeholder: one makes it dispatch on creation, and its plugins' appended
// paragraph then reads as an edit of the file

// a new source or a new file: the description is read again from it
const { mutate: change, isLoading: changing } = useMutation({
  mutation: async (save: () => Promise<unknown>) => {
    await save();
    return LoadListing(listing.workspaceId, listing.modId);
  },
  onSuccess: (next) => {
    markdown.value = next?.readmeMd ?? "";
    bbcode.value = next?.readmeBbcode ?? "";
  },
  // a description file is the mod's, not config.json's: Publish reads it again itself
  onSettled: () => queryCache.invalidateQueries({ key: releaseKeys.mod(listing.workspaceId, listing.modId) }),
});
function setSource(source: string, rel = ""): void {
  change(() => SetWorkspaceModDescription(listing.workspaceId, listing.modId, source, rel));
}

async function pickSource(): Promise<void> {
  const path = await pickFile("The description's file, in the mod", "*.md; *.bbcode; *.txt", listing.root);
  if (path) setSource(path.endsWith(".md") ? "markdown" : "bbcode", path);
}
const sources = computed<DropdownMenuItem[]>(() => [
  { label: `Markdown, ${listing.descMdRel}`, onSelect: () => setSource("markdown") },
  { label: `BBCode, ${listing.descBbRel}`, onSelect: () => setSource("bbcode") },
  { label: "Another file in the mod…", onSelect: pickSource },
  { label: "Steam's own text", onSelect: () => setSource("steam") },
]);
</script>

<template>
  <div class="flex min-h-0 w-170 shrink-0 flex-col gap-2.5 pt-4.5 pb-5">
    <div class="flex items-center gap-2">
      <span class="text-13 font-semibold">Description</span>
      <span class="min-w-0 truncate font-mono text-11 text-dimmed">{{ summary.descRel }}</span>
      <span class="grow" />
      <UTabs v-if="editable" v-model="view" :items="views" :content="false" />
    </div>
    <DescriptionSource :summary="summary">
      <UButton
        v-if="summary.descRel && !summary.descExists"
        :label="`Create ${summary.descRel}`"
        variant="link"
        :loading="changing"
        class="me-2 h-5 p-0 text-12"
        @click="change(() => CreateDescriptionFile(listing.workspaceId, listing.modId))"
      />
      <UDropdownMenu :items="sources">
        <UButton label="Change source" variant="link" class="h-5 p-0 text-12" />
      </UDropdownMenu>
    </DescriptionSource>
    <div v-if="editable" class="flex min-h-0 grow flex-col overflow-hidden rounded-lg border border-default bg-muted">
      <UEditor
        v-if="view === 'edit' && fromMarkdown"
        v-slot="{ editor }"
        v-model="markdown"
        content-type="markdown"
        :mention="false"
        :starter-kit="{ heading: { levels: [1, 2, 3] }, underline: false }"
        class="flex min-h-0 grow flex-col"
        :ui="{ content: 'min-h-0 grow overflow-auto bg-(--bg-0)' }"
      >
        <div class="flex h-9 shrink-0 items-center gap-0.5 border-b border-default bg-elevated px-2">
          <UEditorToolbar :editor="editor" :items="MARKDOWN_TOOLBAR" />
          <span class="grow" />
          <span class="min-w-0 truncate pe-1.5 text-11 text-dimmed">Markdown -> BBCode on upload</span>
        </div>
      </UEditor>
      <UTextarea
        v-else-if="view === 'edit'"
        v-model="bbcode"
        variant="none"
        class="grow bg-(--bg-0)"
        :ui="{ base: 'h-full resize-none px-4.5 py-4 font-mono text-12 md:text-12' }"
      />
      <template v-else-if="sent">
        <UEditor
          v-if="view === 'preview'"
          :model-value="sent.shown"
          content-type="markdown"
          :editable="false"
          :mention="false"
          class="min-h-0 grow"
          :ui="{ content: 'h-full overflow-auto bg-(--bg-0)' }"
        />
        <pre v-else class="min-h-0 grow overflow-auto bg-(--bg-0) px-4.5 py-4 font-mono text-12 whitespace-pre-wrap">{{
          sent.text
        }}</pre>
        <p v-if="sent.notes?.length" class="border-t border-default px-3 py-2 text-11 text-warning">
          {{ sent.notes.join(" ") }}
        </p>
      </template>
    </div>
  </div>
</template>
