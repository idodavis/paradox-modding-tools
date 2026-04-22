<script lang="ts">
  import type { Snippet } from "svelte";
  import LangThemeSelect from "../form-controls/LangThemeSelect.svelte";
  import DiffView from "./DiffView.svelte";
  import { ReadFileContent } from "@services/fileservice";

  let {
    oldFile = "",
    newFile = "",
    oldFileName: customOldFileName,
    newFileName: customNewFileName,
    originalLabelClass = "bg-primary/10 text-primary",
    modifiedLabelClass = "bg-secondary/10 text-secondary",
    hasPrev = false,
    hasNext = false,
    navLabel,
    onPrev,
    onNext,
    onFullscreen,
    extraHeader,
    renderSideBySide = $bindable(true),
  }: {
    oldFile?: string;
    newFile?: string;
    oldFileName?: string;
    newFileName?: string;
    originalLabelClass?: string;
    modifiedLabelClass?: string;
    hasPrev?: boolean;
    hasNext?: boolean;
    navLabel?: string;
    onPrev?: () => void;
    onNext?: () => void;
    onFullscreen?: () => void;
    extraHeader?: Snippet;
    renderSideBySide?: boolean;
  } = $props();

  let originalContent = $state("");
  let modifiedContent = $state("");
  let error = $state<string | null>(null);

  $effect(() => {
    if (!oldFile || !newFile) {
      originalContent = "";
      modifiedContent = "";
      return;
    }
    (async () => {
      try {
        error = null;
        const [o, m] = await Promise.all([ReadFileContent(oldFile), ReadFileContent(newFile)]);
        originalContent = o;
        modifiedContent = m;
      } catch (e) {
        error = e instanceof Error ? e.message : String(e);
      }
    })();
  });

  const oldFileName = $derived(customOldFileName ?? "File A");
  const newFileName = $derived(customNewFileName ?? "File B");
  const oldFileBaseName = $derived(oldFile ? (oldFile.split(/[/\\]/).at(-1) ?? "") : "");
  const newFileBaseName = $derived(newFile ? (newFile.split(/[/\\]/).at(-1) ?? "") : "");
  const showNav = $derived(hasPrev || hasNext);
</script>

<div class="flex flex-col h-full overflow-hidden bg-base-100">
  <div class="px-3 py-2 border-b border-base-content/20 bg-base-200 flex flex-col gap-2 shrink-0">
    <div class="flex items-center justify-between gap-2">
      {#if showNav}
        <div class="flex items-center gap-1">
          <button
            type="button"
            class="btn btn-sm btn-outline btn-accent"
            disabled={!hasPrev}
            onclick={onPrev}
            title="Previous file">←</button
          >
          {#if navLabel}
            <span class="text-xs font-medium tabular-nums px-1 text-base-content/70">{navLabel}</span>
          {/if}
          <button
            type="button"
            class="btn btn-sm btn-outline btn-accent"
            disabled={!hasNext}
            onclick={onNext}
            title="Next file">→</button
          >
        </div>
      {/if}
      <div class="flex items-center gap-2 {showNav ? '' : 'ml-auto'}">
        <span class="text-xs text-base-content/50">View mode</span>
        <div class="join">
          <button
            type="button"
            class="join-item btn btn-sm btn-outline"
            class:btn-primary={!renderSideBySide}
            onclick={() => (renderSideBySide = false)}>Unified</button
          >
          <button
            type="button"
            class="join-item btn btn-sm btn-outline"
            class:btn-primary={renderSideBySide}
            onclick={() => (renderSideBySide = true)}>Split</button
          >
        </div>
        {#if onFullscreen}
          <button type="button" class="btn btn-sm btn-outline" onclick={onFullscreen} title="Expand to fullscreen"
            >⛶</button
          >
        {/if}
      </div>
    </div>
    <div class="flex items-center gap-2 flex-wrap">
      {#if extraHeader}
        {@render extraHeader()}
      {/if}
      <LangThemeSelect />
    </div>
  </div>

  {#if error}
    <div class="flex items-center justify-center flex-1 text-error p-4 text-sm">Error: {error}</div>
  {:else}
    <DiffView
      {originalContent}
      {modifiedContent}
      {renderSideBySide}
      originalLabel={oldFileName}
      modifiedLabel={newFileName}
      originalFileName={oldFileBaseName && oldFileBaseName !== oldFileName ? oldFileBaseName : undefined}
      modifiedFileName={newFileBaseName && newFileBaseName !== newFileName ? newFileBaseName : undefined}
      {originalLabelClass}
      {modifiedLabelClass}
      class="flex-1 min-h-0"
    />
  {/if}
</div>
