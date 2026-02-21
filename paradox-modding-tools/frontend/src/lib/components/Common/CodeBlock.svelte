<script lang="ts">
  import { onDestroy } from "svelte";
  import Dialog from "../bits/Dialog.svelte";
  import LangThemeSelect from "./LangThemeSelect.svelte";
  import Icon from "@iconify/svelte";
  import { CopyToClipboard } from "@services/clipboardservice";
  import { EditorCtx } from "@stores/monaco.svelte";

  let {
    content,
    filename = "No File",
    placeholder = "Missing content",
    showCopyButton = true,
    showFullScreenButton = true,
    hideHeader = false,
    class: codeBlockClass = "",
  }: {
    content: string;
    filename: string;
    placeholder?: string;
    showCopyButton?: boolean;
    showFullScreenButton?: boolean;
    hideHeader?: boolean;
    class?: string;
  } = $props();

  let fullscreenOpen = $state(false);

  const ctx = new EditorCtx({
    readOnly: true,
    options: { renderLineHighlight: "line" },
  });
  const fullCtx = new EditorCtx({
    readOnly: true,
    options: { renderLineHighlight: "line" },
  });

  const name = $derived(
    typeof filename === "string" ? filename : "Select a file",
  );

  onDestroy(() => {
    ctx.dispose();
    fullCtx.dispose(); // ownedModel=false so model not double-disposed
  });

  $effect(() => ctx.upsert(content ?? ""));
  $effect(() => {
    fullCtx.model = ctx.model;
  });

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
              onclick={() => (fullscreenOpen = !fullscreenOpen)}
            >
              <Icon
                icon={isFullscreen ? "mdi:fullscreen-exit" : "mdi:fullscreen"}
              />
            </button>
          {/if}
        </div>
      {/if}
    </div>
  </div>
{/snippet}

<div
  class="code-block-root flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden border border-base-300 bg-dark-input {codeBlockClass}"
>
  {#if !hideHeader}
    <div class="px-3 py-2 bg-base-300 border-b border-base-content/20">
      {@render headerContent(false)}
    </div>
  {/if}
  <div class="code-block-editor relative min-h-0 flex-1 overflow-hidden">
    <div class="absolute inset-0" bind:this={ctx.host}></div>
    {#if !content}
      <p class="pointer-events-none absolute left-4 top-3 opacity-60">
        {placeholder}
      </p>
    {/if}
  </div>
</div>

<Dialog
  bind:open={fullscreenOpen}
  size="fullscreen"
  contentProps={{ class: "!p-0 bg-dark-input flex flex-col" }}
>
  {#snippet title()}
    <span class="sr-only">Editing {name}</span>
  {/snippet}

  {#snippet description()}
    <span class="sr-only">Code Editor Fullscreen</span>
  {/snippet}

  <div class="px-3 py-2 bg-base-300 border-b border-base-content/20 shrink-0">
    {@render headerContent(true)}
  </div>

  <div class="flex-1 min-h-0 relative">
    <div class="absolute inset-0" bind:this={fullCtx.host}></div>
  </div>
</Dialog>
