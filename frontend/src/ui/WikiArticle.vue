<!--
  A wiki article: its HTML with the article styles, a table of contents over its headings, and find, which can start
  with a text its holder gives.
-->
<script setup lang="ts">
import { computed, shallowRef, useTemplateRef, watch } from "vue";
import { useEventListener, useLocalStorage } from "@vueuse/core";
import { Browser } from "@wailsio/runtime";

import type { Page } from "@services/wiki/models";

import FindBox from "./FindBox.vue";
import ScriptSnippet from "./ScriptSnippet.vue";

const { page, find = "" } = defineProps<{ page: Page; find?: string }>();

const BLOCKS = "p, li, h1, h2, h3, h4, h5, h6, td, th, pre, blockquote";
const LEVELS: Record<number, string> = {
  1: "mt-2 ps-2.5 text-12 font-semibold text-highlighted",
  2: "mt-2 ps-2.5 text-12 font-semibold text-highlighted",
  3: "ps-5.5 text-12 text-dimmed",
  4: "ps-8 text-11 text-dimmed",
};
const MARKED = ["bg-(--accent-soft)", "rounded-sm", "ring-1", "ring-primary"];

const body = useTemplateRef("body");
const content = useTemplateRef("content");
const contents = useTemplateRef("contents");
const term = shallowRef("");
const contentsHidden = useLocalStorage("pmt.wiki.contentsHidden", false);
const headings = shallowRef<HTMLElement[]>([]);
const blocks = shallowRef<HTMLElement[]>([]);
const active = shallowRef(-1);
const texts = shallowRef<string[]>([]);
// the article's code blocks, each with its text, which a snippet draws inside it
const codeBlocks = shallowRef<{ pre: HTMLElement; text: string }[]>([]);
const panes = computed(() => [
  ...(headings.value.length && !contentsHidden.value
    ? [{ id: "toc", slot: "toc", order: 1, sizeUnit: "px" as const, defaultSize: 180, minSize: 160, maxSize: 320 }]
    : []),
  { id: "article", slot: "article", order: 2 },
]);
let marked: HTMLElement | undefined;

watch(
  [content, () => page.html],
  () => {
    const root = content.value;
    if (!root) return;
    clear();
    headings.value = [...root.querySelectorAll<HTMLElement>("h1, h2, h3, h4")];
    blocks.value = [...root.querySelectorAll<HTMLElement>(BLOCKS)].filter((block) => !block.querySelector(BLOCKS));
    texts.value = blocks.value.map((block) => block.textContent ?? "");
    codeBlocks.value = [...root.querySelectorAll("pre")].map((pre) => ({ pre, text: pre.textContent ?? "" }));
    // the snippets take the place of the code blocks' own text, which find has kept
    for (const { pre } of codeBlocks.value) pre.replaceChildren();
    body.value?.scrollTo({ top: 0 });
    active.value = -1;
  },
  { flush: "post" },
);
// a text the holder asks to find is found once the article's texts are read
watch(
  [() => find, texts],
  () => {
    if (find) term.value = find;
  },
  { flush: "post" },
);
useEventListener(body, "scroll", markActive, { passive: true });
// the contents follow the reading: the heading being read stays in their view
watch(active, (index) =>
  contents.value?.querySelector(`[data-heading="${index}"]`)?.scrollIntoView({ block: "nearest" }),
);

/** Marks the last heading that has scrolled past the top of the article. */
function markActive(): void {
  const top = (body.value?.getBoundingClientRect().top ?? 0) + 24;
  active.value = headings.value.findLastIndex((heading) => heading.getBoundingClientRect().top <= top);
}

/** Marks the text at the given index as the find's current hit and scrolls to it. */
function show(index: number): void {
  marked?.classList.remove(...MARKED);
  marked = blocks.value[index];
  marked?.classList.add(...MARKED);
  marked?.scrollIntoView({ block: "center" });
}

/** Removes the find's mark and its term. */
function clear(): void {
  marked?.classList.remove(...MARKED);
  marked = undefined;
  term.value = "";
}

/** Follows a link: an anchor scrolls, a web address opens outside, and a link to a wiki page is left to the holder. */
function follow(event: MouseEvent): void {
  const link = event.target instanceof Element ? event.target.closest("a") : null;
  const href = link?.getAttribute("href");
  if (!link || !href) return;
  event.preventDefault();
  if (link.dataset.page) return;
  if (href.startsWith("#")) content.value?.querySelector(`#${CSS.escape(href.slice(1))}`)?.scrollIntoView();
  else if (/^https?:/.test(href)) void Browser.OpenURL(href);
}
</script>

<template>
  <article class="flex min-h-0 min-w-0 flex-1 flex-col">
    <div class="flex shrink-0 justify-end px-3 pt-2.5 pb-2">
      <FindBox v-model:term="term" :texts="texts" @hit="show" @close="clear" />
    </div>
    <USplitter :items="panes" auto-save-id="pmt.wiki" class="min-h-0 flex-1 border-t border-default">
      <template #toc>
        <nav
          ref="contents"
          class="flex min-w-0 flex-1 flex-col overflow-auto border-e border-default pt-3 ps-1 pe-2 [scrollbar-gutter:stable]"
        >
          <div class="flex items-center ps-2.5 pb-1">
            <span class="grow text-11 font-medium text-dimmed">On this page</span>
            <UTooltip text="Hide the contents">
              <UButton
                icon="i-lucide-panel-left-close"
                variant="ghost"
                size="xs"
                square
                aria-label="Hide the contents"
                @click="contentsHidden = true"
              />
            </UTooltip>
          </div>
          <button
            v-for="(heading, index) in headings"
            :key="index"
            type="button"
            class="shrink-0 border-s-2 py-1.25 pe-2.5 text-start leading-4 line-clamp-2"
            :data-heading="index"
            :title="heading.textContent ?? ''"
            :class="[
              index === active ? 'border-primary text-highlighted' : 'border-default',
              LEVELS[Number(heading.tagName.slice(1))],
            ]"
            @click="heading.scrollIntoView({ block: 'start' })"
          >
            {{ heading.textContent }}
          </button>
        </nav>
      </template>
      <template #article>
        <nav
          v-if="headings.length && contentsHidden"
          class="flex shrink-0 flex-col items-center border-e border-default px-1 pt-3"
        >
          <UTooltip text="Show the contents">
            <UButton
              icon="i-lucide-panel-left-open"
              variant="ghost"
              size="xs"
              square
              aria-label="Show the contents"
              @click="contentsHidden = false"
            />
          </UTooltip>
        </nav>
        <div ref="body" class="min-h-0 min-w-0 flex-1 overflow-x-hidden overflow-y-auto px-3.5 py-3 wrap-break-word">
          <!-- eslint-disable-next-line vue/no-v-html -- sanitized by Go -->
          <div ref="content" class="art max-w-none" @click="follow" v-html="page.html" />
          <Teleport v-for="(codeBlock, index) in codeBlocks" :key="index" :to="codeBlock.pre">
            <ScriptSnippet
              :text="codeBlock.text"
              language="paradox-script"
              class="my-3 rounded-lg border border-default px-3 pb-1.5"
            />
          </Teleport>
        </div>
      </template>
    </USplitter>
  </article>
</template>
