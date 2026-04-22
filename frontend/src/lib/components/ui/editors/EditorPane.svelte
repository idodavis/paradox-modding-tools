<script lang="ts">
  import { Dialog } from "@components";
  import EditorView from "./EditorView.svelte";
  import LangThemeSelect from "../form-controls/LangThemeSelect.svelte";
  import Icon from "@iconify/svelte";
  import { CopyToClipboard } from "@services/clipboardservice";

  let {
    content,
    filename = "File",
    placeholder = "Missing content",
    showCopyButton = true,
    showFullScreenButton = true,
    hideHeader = false,
    firstLineNumber = 1,
    class: codeBlockClass = "",
  }: {
    content: string;
    filename?: string;
    placeholder?: string;
    showCopyButton?: boolean;
    showFullScreenButton?: boolean;
    hideHeader?: boolean;
    firstLineNumber?: number;
    class?: string;
  } = $props();

  let fullscreenOpen = $state(false);

  const name = $derived(filename || "Select a file");

  function copy() {
    CopyToClipboard(String(content ?? ""));
  }
</script>

{#snippet headerContent(isFullscreen: boolean)}
  <div class="flex items-center justify-between gap-2 h-10">
    <span class="truncate font-semibold text-accent min-w-0">{name}</span>

    <div class="flex items-center gap-2">
      <LangThemeSelect />

      {#if showCopyButton || showFullScreenButton}
        <div class="flex gap-1">
          {#if showCopyButton}
            <button type="button" class="btn btn-soft btn-secondary btn-sm" onclick={copy}>
              <Icon icon="mdi:content-copy" />
            </button>
          {/if}
          {#if showFullScreenButton}
            <button
              type="button"
              class="btn btn-soft btn-secondary btn-sm"
              onclick={() => (fullscreenOpen = !fullscreenOpen)}
            >
              <Icon icon={isFullscreen ? "mdi:fullscreen-exit" : "mdi:fullscreen"} />
            </button>
          {/if}
        </div>
      {/if}
    </div>
  </div>
{/snippet}

<div class="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden border border-base-300 bg-dark-input {codeBlockClass}">
  {#if !hideHeader}
    <div class="px-3 py-2 bg-base-300 border-b border-base-content/20">
      {@render headerContent(false)}
    </div>
  {/if}
  <div class="relative flex min-h-0 flex-1 flex-col overflow-hidden">
    {#if content}
      <EditorView {content} {firstLineNumber} {placeholder} />
    {:else}
      <div class="flex flex-1 items-center justify-center p-4 text-base-content/50">
        <p>{placeholder}</p>
      </div>
    {/if}
  </div>
</div>

<Dialog bind:open={fullscreenOpen} size="fullscreen" contentProps={{ class: "!p-0 bg-dark-input flex flex-col" }}>
  {#snippet title()}
    <span class="sr-only">Editing {name}</span>
  {/snippet}

  {#snippet description()}
    <span class="sr-only">Code Editor Fullscreen</span>
  {/snippet}

  <div class="px-3 py-2 bg-base-300 border-b border-base-content/20 shrink-0">
    {@render headerContent(true)}
  </div>

  <div class="flex-1 overflow-hidden">
    {#if content}
      <EditorView {content} {firstLineNumber} {placeholder} />
    {:else}
      <div class="flex h-full items-center justify-center p-4 text-base-content/50">
        <p>{placeholder}</p>
      </div>
    {/if}
  </div>
</Dialog>
