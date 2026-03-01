<script lang="ts">
  import { onDestroy } from "svelte";
  import { DiffCtx } from "@stores/monaco.svelte";

  let {
    originalContent = "",
    modifiedContent = "",
    originalLabel,
    modifiedLabel,
    originalFileName,
    modifiedFileName,
    originalLabelClass = "bg-primary/10 text-primary",
    modifiedLabelClass = "bg-secondary/10 text-secondary",
    origFirstLine = 1,
    modFirstLine = 1,
    renderSideBySide = true,
    class: className = "",
  }: {
    originalContent?: string;
    modifiedContent?: string;
    originalLabel?: string;
    modifiedLabel?: string;
    originalFileName?: string;
    modifiedFileName?: string;
    originalLabelClass?: string;
    modifiedLabelClass?: string;
    origFirstLine?: number;
    modFirstLine?: number;
    renderSideBySide?: boolean;
    class?: string;
  } = $props();

  const ctx = new DiffCtx();
  onDestroy(() => ctx.dispose());

  $effect(() => {
    ctx.renderSideBySide = renderSideBySide;
    ctx.origFirstLine = origFirstLine;
    ctx.modFirstLine = modFirstLine;
    ctx.setContent(originalContent, modifiedContent);
  });
</script>

<div class="flex flex-col h-full overflow-hidden {className}">
  {#if originalLabel || modifiedLabel}
    <div class="flex shrink-0 border-b-2 border-base-content/15">
      <div class="flex-1 py-2 px-3 text-sm font-semibold {originalLabelClass}">
        {originalLabel ?? ""}
        {#if originalFileName}
          <span class="font-normal text-base-content/50 ml-1">{originalFileName}</span>
        {/if}
      </div>
      <div class="flex-1 py-2 px-3 text-sm font-semibold {modifiedLabelClass}" class:text-right={!renderSideBySide}>
        {modifiedLabel ?? ""}
        {#if modifiedFileName}
          <span class="font-normal text-base-content/50 ml-1">{modifiedFileName}</span>
        {/if}
      </div>
    </div>
  {/if}

  <div class="min-h-0 flex-1 relative">
    {#if !originalContent && !modifiedContent}
      <div class="absolute inset-0 flex items-center justify-center text-base-content/40 text-sm select-none">
        Select a row to view the diff
      </div>
    {/if}
    <div class="absolute inset-0" bind:this={ctx.host}></div>
  </div>
</div>
