<script lang="ts">
  import { onMount } from "svelte";
  import Icon from "@iconify/svelte";
  import type { Highlighter } from "shiki";
  import { CopyToClipboard } from "@services/clipboardservice";
  import { getHighlighter } from "@utils/shiki";
  import { Dialog } from "@components";

  let {
    content,
    filename = "No File",
    language = "hcl",
    placeholder = "Missing content",
    showCopyButton = true,
    showFullScreenButton = true,
    hideHeader = false,
    startLine = 1,
    highlightLines = [],
    highlightColor = "bg-warning/20",
    class: codeBlockClass = "",
  }: {
    content: string;
    filename: string;
    language?: string;
    placeholder?: string;
    showCopyButton?: boolean;
    showFullScreenButton?: boolean;
    hideHeader?: boolean;
    startLine?: number;
    highlightLines?: number[];
    highlightColor?: string;
    class?: string;
  } = $props();

  let highlighter = $state<Highlighter | null>(null);
  let html = $state<string | null>(null);
  let fullscreenOpen = $state(false);

  const name = $derived(
    typeof filename === "string" ? filename : "Select a file",
  );

  onMount(() => {
    getHighlighter().then((h) => (highlighter = h));
  });

  const createLinePrefixTransformer = (start: number) => ({
    name: "line-prefix",
    line(node: { properties?: Record<string, unknown> }, line: number) {
      if (node.properties)
        node.properties["data-prefix"] = String(start + line - 1);
    },
  });

  const createLineHighlightTransformer = (lines: number[], color: string) => ({
    name: "line-highlight",
    line(node: { properties?: Record<string, unknown> }, line: number) {
      if (lines.includes(line) && node.properties) {
        const existing = (node.properties.class as string) || "";
        node.properties.class = (existing + " " + color).trim();
      }
    },
  });

  $effect(() => {
    const h = highlighter;
    const c = content;
    if (!h || !c) {
      html = null;
      return;
    }
    Promise.resolve(
      h.codeToHtml(c, {
        lang: language,
        theme: "one-dark-pro",
        transformers: [
          createLinePrefixTransformer(startLine),
          createLineHighlightTransformer(highlightLines, highlightColor),
        ],
      }),
    )
      .then((r) => (html = r))
      .catch(() => (html = null));
  });

  function copy() {
    CopyToClipboard(String(content ?? ""));
  }

  const lines = $derived((content ?? "").split("\n"));
</script>

<div
  class="code-block-root flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden border border-base-300 bg-dark-input {codeBlockClass}"
>
  {#if !hideHeader}
    <div class="px-3 py-2 bg-base-300 border-b border-base-content/20">
      <div class="flex items-center justify-between gap-2 h-10">
        <span class="truncate font-semibold text-accent min-w-0">{name}</span>
        {#if (showCopyButton || showFullScreenButton) && content}
          <div class="flex gap-1">
            {#if showCopyButton}
              <button
                type="button"
                class="btn btn-soft btn-secondary btn-sm"
                onclick={copy}
              >
                <Icon icon="mdi:content-copy" />
              </button>
            {/if}
            {#if showFullScreenButton}
              <button
                type="button"
                class="btn btn-soft btn-secondary btn-sm"
                onclick={() => (fullscreenOpen = true)}
              >
                <Icon icon="mdi:fullscreen" />
              </button>
            {/if}
          </div>
        {/if}
      </div>
    </div>
  {/if}
  <div class="code-block-editor min-h-0 flex-1 overflow-auto">
    {#if content}
      {#if html}
        <div class="code-block-lines min-w-max">{@html html}</div>
      {:else}
        <div class="mockup-code w-full min-w-max bg-dark-input">
          {#each lines as line, i}
            <pre
              data-prefix={String(startLine + i)}
              class={highlightLines.includes(i + 1) ? highlightColor : ""}><code
                >{line}</code
              ></pre>
          {/each}
        </div>
      {/if}
    {:else}
      <p class="p-4">{placeholder}</p>
    {/if}
  </div>
</div>

<Dialog
  bind:open={fullscreenOpen}
  size="fullscreen"
  contentProps={{ class: "z-50 bg-dark-input flex flex-col overflow-auto" }}
>
  {#snippet title()}
    <div
      class="flex justify-between items-center border-b border-base-content/20"
    >
      <h3 class="font-bold truncate text-accent">{name}</h3>
      <button
        type="button"
        class="btn btn-circle btn-ghost"
        onclick={() => (fullscreenOpen = false)}>✕</button
      >
    </div>
  {/snippet}
  {#snippet description()}
    <div class="min-h-0 flex-1 overflow-auto text-left [&_pre]:!p-2">
      {#if html}
        <div class="code-block-lines min-w-max">{@html html}</div>
      {:else}
        <div class="mockup-code w-full min-w-max bg-dark-input">
          {#each lines as line, i}
            <pre
              data-prefix={String(i + 1)}
              class={highlightLines.includes(i + 1) ? highlightColor : ""}><code
                >{line}</code
              ></pre>
          {/each}
        </div>
      {/if}
    </div>
  {/snippet}
</Dialog>

<style>
  :global(.code-block-lines .line) {
    position: relative;
    padding-left: 2.5rem;
  }
  :global(.code-block-lines .line::before) {
    content: attr(data-prefix);
    position: absolute;
    left: 0;
    width: 1.5rem;
    text-align: right;
    font-variant-numeric: tabular-nums;
    color: inherit;
    opacity: 0.6;
    user-select: none;
  }
</style>
