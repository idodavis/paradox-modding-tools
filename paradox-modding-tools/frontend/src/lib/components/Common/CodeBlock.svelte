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
    class: codeBlockClass = "",
  }: {
    content: string;
    filename: string;
    language?: string;
    placeholder?: string;
    showCopyButton?: boolean;
    showFullScreenButton?: boolean;
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

  $effect(() => {
    const h = highlighter;
    const c = content;
    if (!h || !c) {
      html = null;
      return;
    }
    Promise.resolve(h.codeToHtml(c, { lang: language, theme: "one-dark-pro" }))
      .then((r) => (html = r))
      .catch(() => (html = null));
  });

  function copy() {
    CopyToClipboard(String(content ?? ""));
  }

  const lines = $derived((content ?? "").split("\n"));
</script>

<div
  class="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden bg-dark-input {codeBlockClass}"
>
  <div class="px-3 py-2 bg-base-300 border-b border-base-content/20">
    <div class="label py-1">
      <span class="label-text font-semibold text-sm">File Content</span>
    </div>
    <div class="flex items-center justify-between gap-2 h-10">
      <span class="truncate font-medium text-sm" title={name}>{name}</span>
      {#if (showCopyButton || showFullScreenButton) && content}
        <div class="flex flex-shrink-0 gap-1">
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
  <div
    class="min-h-0 flex-1 overflow-auto text-left p-1 [&_pre]:whitespace-pre [&_pre]:!m-0 [&_pre]:!p-2"
  >
    {#if content}
      {#if html}
        <div class="min-w-max p-2">{@html html}</div>
      {:else}
        <div class="mockup-code w-full min-w-max bg-dark-input">
          {#each lines as line, i}
            <pre data-prefix={String(i + 1)}><code>{line}</code></pre>
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
  contentProps={{
    class:
      "fixed inset-0 z-50 w-full h-full bg-dark-input flex-col overflow-auto",
  }}
>
  {#snippet title()}
    <div
      class="flex justify-between items-center p-2 border-b border-base-content/20"
    >
      <h3 class="font-bold truncate pl-2">{name}</h3>
      <button
        class="btn btn-circle btn-ghost btn-sm"
        onclick={() => (fullscreenOpen = false)}>✕</button
      >
    </div>
  {/snippet}
  {#snippet description()}
    <div class="min-h-0 flex-1 overflow-auto text-left [&_pre]:!p-2">
      {#if html}
        <div class="min-w-max p-2">{@html html}</div>
      {:else}
        <div class="mockup-code w-full min-w-max bg-dark-input">
          {#each lines as line, i}
            <pre data-prefix={String(i + 1)}><code>{line}</code></pre>
          {/each}
        </div>
      {/if}
    </div>
  {/snippet}
</Dialog>
