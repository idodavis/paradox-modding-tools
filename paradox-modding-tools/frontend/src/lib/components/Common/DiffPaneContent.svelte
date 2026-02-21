<script lang="ts">
  import type { Snippet } from "svelte";
  import { onDestroy } from "svelte";
  import LangThemeSelect from "./LangThemeSelect.svelte";
  import { ReadFileContent } from "@services/fileservice";
  import { DiffCtx } from "@stores/monaco.svelte";

  let {
    oldFile = "",
    newFile = "",
    oldFileName: customOldFileName,
    newFileName: customNewFileName,
    oldFileColor = "text-primary",
    newFileColor = "text-secondary",
    hasPrev = false,
    hasNext = false,
    navLabel,
    onPrev,
    onNext,
    onFullscreen,
    extraHeader,
  }: {
    oldFile?: string;
    newFile?: string;
    oldFileName?: string;
    newFileName?: string;
    oldFileColor?: string;
    newFileColor?: string;
    hasPrev?: boolean;
    hasNext?: boolean;
    navLabel?: string;
    onPrev?: () => void;
    onNext?: () => void;
    onFullscreen?: () => void;
    extraHeader?: Snippet;
  } = $props();

  let error = $state<string | null>(null);

  const ctx = new DiffCtx();

  onDestroy(() => ctx.dispose());

  const oldFileName = $derived(customOldFileName ?? "File A");
  const newFileName = $derived(customNewFileName ?? "File B");
  const oldFileBaseName = $derived(
    oldFile ? (oldFile.split(/[/\\]/).at(-1) ?? "") : "",
  );
  const newFileBaseName = $derived(
    newFile ? (newFile.split(/[/\\]/).at(-1) ?? "") : "",
  );

  $effect(() => {
    if (!oldFile || !newFile) return;
    (async () => {
      try {
        error = null;
        const [o, m] = await Promise.all([
          ReadFileContent(oldFile),
          ReadFileContent(newFile),
        ]);
        ctx.setContent(o, m);
      } catch (e) {
        error = e instanceof Error ? e.message : String(e);
      }
    })();
  });
</script>

<div class="flex flex-col h-full overflow-hidden bg-base-100">
  <!-- Toolbar header -->
  <div
    class="px-3 py-2 border-b border-base-content/20 bg-base-200 flex flex-col gap-2 shrink-0"
  >
    <!-- Navigation + controls row -->
    <div class="flex items-center justify-between gap-2">
      <!-- Prev / Next -->
      <div class="flex items-center gap-1">
        <button
          type="button"
          class="btn btn-sm btn-outline btn-accent"
          disabled={!hasPrev}
          onclick={onPrev}
          aria-label="Previous file"
          title="Previous file">←</button
        >
        {#if navLabel}
          <span
            class="text-xs font-medium tabular-nums px-1 text-base-content/70"
            >{navLabel}</span
          >
        {/if}
        <button
          type="button"
          class="btn btn-sm btn-outline btn-accent"
          disabled={!hasNext}
          onclick={onNext}
          aria-label="Next file"
          title="Next file">→</button
        >
      </div>
      <!-- Right controls -->
      <div class="flex items-center gap-1.5">
        <div class="join">
          <button
            type="button"
            class="join-item btn btn-sm btn-outline"
            class:btn-primary={!ctx.renderSideBySide}
            onclick={() => (ctx.renderSideBySide = false)}>Unified</button
          >
          <button
            type="button"
            class="join-item btn btn-sm btn-outline"
            class:btn-primary={ctx.renderSideBySide}
            onclick={() => (ctx.renderSideBySide = true)}>Split</button
          >
        </div>
        <LangThemeSelect />
        {#if onFullscreen}
          <button
            type="button"
            class="btn btn-sm btn-outline"
            onclick={onFullscreen}
            aria-label="Expand to fullscreen"
            title="Expand to fullscreen">⛶</button
          >
        {/if}
      </div>
    </div>

    <!-- Optional extra header (e.g. diff-side tabs injected by merge) -->
    {#if extraHeader}
      {@render extraHeader()}
    {/if}

    <!-- File labels with base filenames -->
    <div class="flex justify-between items-start text-xs min-w-0 gap-2">
      <div class="flex flex-col min-w-0">
        <span class="truncate font-semibold {oldFileColor}" title={oldFileName}
          >{oldFileName}</span
        >
        {#if oldFileBaseName && oldFileBaseName !== oldFileName}
          <span class="truncate text-base-content/50" title={oldFile}
            >{oldFileBaseName}</span
          >
        {/if}
      </div>
      <div class="flex flex-col min-w-0 items-end">
        <span
          class="truncate text-right font-semibold {newFileColor}"
          title={newFileName}>{newFileName}</span
        >
        {#if newFileBaseName && newFileBaseName !== newFileName}
          <span class="truncate text-base-content/50" title={newFile}
            >{newFileBaseName}</span
          >
        {/if}
      </div>
    </div>
  </div>

  <!-- Editor area -->
  <div class="min-h-0 flex-1 relative">
    {#if !oldFile || !newFile}
      <div
        class="absolute inset-0 flex items-center justify-center text-base-content/40 text-sm select-none"
      >
        Select a row to view the diff
      </div>
    {:else if error}
      <div
        class="absolute inset-0 flex items-center justify-center text-error p-4 text-sm"
      >
        Error: {error}
      </div>
    {/if}
    <div class="absolute inset-0" bind:this={ctx.host}></div>
  </div>
</div>
