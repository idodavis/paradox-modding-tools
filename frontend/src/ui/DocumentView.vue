<!--
  A document Go sanitized: its HTML, a rail of its headings, a find that floats
  over it, and the one follower of its links.
-->
<script setup lang="ts">
import { computed, shallowRef, useTemplateRef, watch } from "vue";
import { useEventListener, useLocalStorage } from "@vueuse/core";
import { Browser } from "@wailsio/runtime";
import { useRoute, useRouter } from "vue-router";

import { useSavedPixelSize } from "../lib/splitter";
import { useWindowsStore } from "../stores/windows";
import FindBox from "./FindBox.vue";
import ScriptSnippet from "./ScriptSnippet.vue";

const {
  html,
  saveId,
  jump = "",
  anchor = "",
} = defineProps<{
  html: string;
  /** Names the surface, so each keeps its own rail width and fold. */
  saveId: string;
  /** A word of a heading. The rail gets a "Jump to" button for the first. */
  jump?: string;
  /** The id of the heading the document opens at. Without one, its top. */
  anchor?: string;
}>();
/** The text the find looks for, which a host may share with its other views. */
const term = defineModel<string>("find", { default: "" });
defineSlots<{
  /** Controls in the rail's header. */
  rail?: () => unknown;
  /** Marks beside the rail's entry of the heading with an id. */
  entry?: (props: { id: string }) => unknown;
  /** A line under the heading with an id, in the document. */
  section?: (props: { id: string }) => unknown;
}>();

const BLOCKS = "p, li, h1, h2, h3, h4, h5, h6, td, th, pre, blockquote";
const HEADING_CLASSES: Record<number, string> = {
  1: "mt-2 ps-2.5 text-12 font-semibold text-highlighted",
  2: "mt-2 ps-2.5 text-12 font-semibold text-highlighted",
  3: "ps-5.5 text-12 text-dimmed",
  4: "ps-8 text-11 text-dimmed",
};
const MARKED = ["bg-(--accent-soft)", "rounded-sm", "ring-1", "ring-primary"];

const router = useRouter();
const route = useRoute();
const windows = useWindowsStore();
const body = useTemplateRef("body");
const content = useTemplateRef("content");
const rail = useTemplateRef("rail");
const railHidden = useLocalStorage(`pmt.document.${saveId}.railHidden`, false);
const railWidth = useSavedPixelSize(useTemplateRef("splitter"), 0, `pmt.document.${saveId}.railWidth`, 180);
const headings = shallowRef<HTMLElement[]>([]);
const blocks = shallowRef<HTMLElement[]>([]);
const texts = shallowRef<string[]>([]);
const active = shallowRef(-1);
const codeBlocks = shallowRef<{ pre: HTMLElement; text: string }[]>([]);
/** Where the section slot is drawn: under each heading that has an id. */
const sections = shallowRef<{ id: string; place: HTMLElement }[]>([]);
const panes = computed(() => [
  ...(headings.value.length && !railHidden.value
    ? [
        {
          id: "rail",
          slot: "rail",
          order: 1,
          sizeUnit: "px" as const,
          defaultSize: railWidth.size(),
          minSize: 160,
          maxSize: 320,
        },
      ]
    : []),
  { id: "document", slot: "document", order: 2 },
]);
const jumpHeading = computed(() =>
  jump ? headings.value.find((heading) => heading.textContent?.includes(jump)) : undefined,
);
let marked: HTMLElement | undefined;
/** Whether the find shows. Ctrl F, its button or a text to find opens it. */
const finding = shallowRef(false);
watch(term, (next) => next && (finding.value = true), { immediate: true });
// An editor that holds the focus keeps Ctrl F for its own find.
defineShortcuts({
  ctrl_f: {
    usingInput: true,
    handler: () => {
      if (!document.activeElement?.closest(".monaco-editor")) finding.value = true;
    },
  },
});

watch(
  [content, () => html],
  () => {
    const root = content.value;
    if (!root) return;
    unmark();
    headings.value = [...root.querySelectorAll<HTMLElement>("h1, h2, h3, h4")];
    blocks.value = [...root.querySelectorAll<HTMLElement>(BLOCKS)].filter((block) => !block.querySelector(BLOCKS));
    texts.value = blocks.value.map((block) => block.textContent ?? "");
    codeBlocks.value = [...root.querySelectorAll("pre")].map((pre) => ({ pre, text: pre.textContent ?? "" }));
    // The snippets replace the code blocks' own text, which find has kept.
    for (const { pre } of codeBlocks.value) pre.replaceChildren();
    sections.value = headings.value
      .filter((heading) => heading.id)
      .map((heading) => {
        const place = document.createElement("div");
        heading.after(place);
        return { id: heading.id, place };
      });
    body.value?.scrollTo({ top: 0 });
    active.value = -1;
    reveal(anchor);
  },
  { flush: "post" },
);
watch(() => anchor, reveal);
useEventListener(body, "scroll", markActive, { passive: true });
watch(active, (index) => rail.value?.querySelector(`[data-heading="${index}"]`)?.scrollIntoView({ block: "nearest" }));

/** markActive marks the last heading that has scrolled past the top. */
function markActive(): void {
  const top = (body.value?.getBoundingClientRect().top ?? 0) + 24;
  active.value = headings.value.findLastIndex((heading) => heading.getBoundingClientRect().top <= top);
}

/** show marks one of the find's hits as the current one and scrolls to it. */
function show(index: number): void {
  unmark();
  marked = blocks.value[index];
  marked?.classList.add(...MARKED);
  marked?.scrollIntoView({ block: "center" });
}

/** reveal scrolls to the part of the document that has the given id, if any. */
function reveal(id: string): void {
  if (id) content.value?.querySelector(`#${CSS.escape(id)}`)?.scrollIntoView();
}

/** unmark removes the find's mark. */
function unmark(): void {
  marked?.classList.remove(...MARKED);
  marked = undefined;
}

/** clear closes the find: its mark and its term go. */
function clear(): void {
  unmark();
  term.value = "";
  finding.value = false;
}

/**
 * follow handles a click on a link. A wiki page opens in the Wiki pane, a tab
 * of the version shown opens through the route, an anchor scrolls, and a web
 * address opens in the browser.
 */
function follow(event: MouseEvent): void {
  const link = event.target instanceof Element ? event.target.closest("a") : null;
  const href = link?.getAttribute("href");
  if (!link || !href) return;
  event.preventDefault();
  const { page, tab } = link.dataset;
  if (page) windows.lookUp({ title: page });
  else if (tab) void router.replace({ query: { ...route.query, tab } });
  else if (href.startsWith("#")) reveal(href.slice(1));
  else if (/^https?:/.test(href)) void Browser.OpenURL(href);
}
</script>

<template>
  <USplitter
    ref="splitter"
    :items="panes"
    class="min-h-0 min-w-0 flex-1"
    @layout="panes.length === 2 && railWidth.keep($event)"
    @dragging="railWidth.save"
  >
    <template #rail>
      <nav
        ref="rail"
        class="flex min-w-0 flex-1 flex-col overflow-auto border-e border-default pt-2 ps-1 pe-2 [scrollbar-gutter:stable]"
      >
        <div class="flex items-center ps-2.5">
          <span class="grow text-11 font-medium text-dimmed">On this page</span>
          <UTooltip text="Hide the contents">
            <UButton
              icon="i-lucide-panel-left-close"
              variant="ghost"
              size="xs"
              square
              aria-label="Hide the contents"
              @click="railHidden = true"
            />
          </UTooltip>
        </div>
        <div class="flex flex-col items-start gap-1 ps-1 pb-1">
          <UButton
            v-if="jumpHeading"
            :label="`Jump to ${jump}`"
            icon="i-lucide-arrow-down-to-line"
            variant="ghost"
            size="xs"
            @click="jumpHeading.scrollIntoView({ block: 'start' })"
          />
          <slot name="rail" />
        </div>
        <button
          v-for="(heading, index) in headings"
          :key="index"
          type="button"
          class="flex shrink-0 items-center gap-1.5 border-s-2 py-1.25 pe-2.5 text-start leading-4"
          :data-heading="index"
          :title="heading.textContent ?? ''"
          :class="[
            index === active ? 'border-primary text-highlighted' : 'border-default',
            HEADING_CLASSES[Number(heading.tagName.slice(1))],
          ]"
          @click="heading.scrollIntoView({ block: 'start' })"
        >
          <!-- the clamp sits inside the padding, which would show a third -->
          <!-- line -->
          <span class="line-clamp-2 min-w-0 grow">{{ heading.textContent }}</span>
          <slot v-if="heading.id" :id="heading.id" name="entry" />
        </button>
      </nav>
    </template>
    <template #document>
      <nav
        v-if="headings.length && railHidden"
        class="flex shrink-0 flex-col items-center border-e border-default px-1 pt-2"
      >
        <UTooltip text="Show the contents">
          <UButton
            icon="i-lucide-panel-left-open"
            variant="ghost"
            size="xs"
            square
            aria-label="Show the contents"
            @click="railHidden = false"
          />
        </UTooltip>
      </nav>
      <div class="relative flex min-h-0 min-w-0 flex-1 flex-col">
        <FindBox
          v-if="finding"
          v-model:term="term"
          :texts="texts"
          autofocus
          class="absolute end-5 top-2 z-10 w-72 shadow-md"
          @hit="show"
          @close="clear"
        />
        <UButton
          v-else
          icon="i-lucide-search"
          variant="ghost"
          size="xs"
          square
          aria-label="Find in this document"
          class="absolute end-5 top-2 z-10 opacity-60 hover:opacity-100"
          @click="finding = true"
        />
        <div ref="body" class="min-h-0 min-w-0 flex-1 overflow-x-hidden overflow-y-auto px-3.5 py-3 wrap-break-word">
          <!-- eslint-disable-next-line vue/no-v-html -- sanitized by Go -->
          <div ref="content" class="art max-w-none" @click="follow" v-html="html" />
          <Teleport v-for="(codeBlock, index) in codeBlocks" :key="index" :to="codeBlock.pre">
            <ScriptSnippet :text="codeBlock.text" class="my-3 rounded-lg border border-default px-3 pb-1.5" />
          </Teleport>
          <template v-if="$slots.section">
            <Teleport v-for="section in sections" :key="section.id" :to="section.place">
              <slot :id="section.id" name="section" />
            </Teleport>
          </template>
        </div>
      </div>
    </template>
  </USplitter>
</template>
