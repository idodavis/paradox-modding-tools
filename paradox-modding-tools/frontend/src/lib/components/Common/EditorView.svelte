<script lang="ts">
  import { onDestroy } from "svelte";
  import { EditorCtx } from "@stores/monaco.svelte";

  let {
    content,
    firstLineNumber = 1,
    placeholder = "Missing Code Content",
    label,
    labelClass = "bg-base-200 text-base-content",
    fileName,
    readOnly = true,
    onContentChange,
    class: className = "",
  } = $props<{
    content: string;
    firstLineNumber?: number;
    placeholder?: string;
    label?: string;
    labelClass?: string;
    fileName?: string;
    readOnly?: boolean;
    onContentChange?: (value: string) => void;
    class?: string;
  }>();

  const ctx = new EditorCtx();
  onDestroy(() => ctx.dispose());

  $effect(() => {
    ctx.upsert(content ?? "");
    ctx.firstLineNumber = firstLineNumber;
    ctx.readOnly = readOnly;
  });

  $effect(() => {
    if (readOnly || !onContentChange || !ctx.model) return () => {};
    const disp = ctx.model.onDidChangeContent(() => {
      if (!ctx.programmatic) onContentChange(ctx.model?.getValue() ?? "");
    });
    return () => disp.dispose();
  });
</script>

<div class="flex min-h-0 flex-1 flex-col h-full {className}">
  {#if label}
    <div class="text-sm font-semibold py-2 px-3 shrink-0 border-b-2 border-base-content/15 {labelClass}">
      {label}
      {#if fileName}
        <span class="font-normal text-base-content/50 ml-1">{fileName}</span>
      {/if}
    </div>
  {/if}
  <div class="relative min-h-0 flex-1 overflow-hidden">
    <div class="absolute inset-0" bind:this={ctx.host}></div>
    {#if !content}
      <p>{placeholder}</p>
    {/if}
  </div>
</div>
