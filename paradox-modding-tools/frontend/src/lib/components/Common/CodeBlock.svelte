<script lang="ts">
  import { onMount } from "svelte";
  import Icon from "@iconify/svelte";
  import {
    type BundledLanguage,
    type BundledTheme,
    type HighlighterGeneric,
  } from "shiki";
  import { CopyToClipboard } from "@services/clipboardservice";
  import { getHighlighter } from "@utils/shiki";

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

  let highlighter = $state<HighlighterGeneric<
    BundledLanguage,
    BundledTheme
  > | null>(null);
  let html = $state<string | null>(null);
  let fullScreenVisible = $state(false);

  onMount(async () => {
    highlighter = await getHighlighter();
  });

  $effect(() => {
    const h = highlighter;
    const c = content;
    const lang = language;
    if (!h || c == null || c === "") return;
    html = h.codeToHtml(c, { lang, theme: "one-dark-pro" });
  });

  const contentLines = $derived(content.split("\n"));
</script>

<div class="flex flex-col p-2 bg-dark-input {codeBlockClass}">
  <div class="p-2 border-b border-base-content/20 flex justify-between gap-2">
    <span>{{ filename }}</span>
    {#if showCopyButton || showFullScreenButton}
      <div class="flex items-center gap-1">
        {#if showCopyButton}
          <button
            onclick={() => CopyToClipboard(content)}
            class="btn btn-soft btn-secondary"
          >
            <Icon icon="mdi:copy" />
          </button>
        {/if}
        {#if showFullScreenButton}
          <button
            onclick={() => (fullScreenVisible = true)}
            class="btn btn-soft btn-secondary"
          >
            <Icon icon="mdi:window-maximize" />
          </button>
        {/if}
      </div>
    {/if}
  </div>
  <div class="mockup-code w-full bg-dark-input">
    {#if content}
      {#each contentLines as line, index}
        <pre data-prefix={index + 1}><code>{line}</code></pre>
      {/each}
    {:else}
      <p class="p-4">{placeholder}</p>
    {/if}
  </div>
  {#if fullScreenVisible}
    <dialog
      id="my_modal_3"
      class="modal fixed inset-0 w-screen h-screen bg-dark-input"
    >
      <div class="modal-box">
        <form method="dialog">
          <button class="btn btn-circle btn-ghost absolute right-2 top-2"
            >✕</button
          >
        </form>
        <div class="mockup-code w-full bg-dark-input">
          <h3 class="text-lg font-bold p-2 border-b border-base-content/20">
            {filename}
          </h3>
          {#if content}
            {#each contentLines as line, index}
              <pre data-prefix={index + 1}><code>{line}</code></pre>
            {/each}
          {:else}
            <p class="p-4">{placeholder}</p>
          {/if}
        </div>
      </div>
    </dialog>
  {/if}
</div>
