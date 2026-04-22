<script lang="ts">
  import type { Snippet } from "svelte";

  let {
    first,
    second,
    secondOpen = true,
    defaultSecondSize = 600,
    fixedSide = "second",
    orientation = "horizontal",
    class: className = "",
  }: {
    first: Snippet;
    second: Snippet;
    secondOpen?: boolean;
    defaultSecondSize?: number;
    fixedSide?: "first" | "second";
    orientation?: "horizontal" | "vertical";
    class?: string;
  } = $props();

  let fixedSize = $state<number | undefined>(undefined);
  const currentSize = $derived(fixedSize ?? defaultSecondSize);
  let dragStart = $state<{ pos: number; startSize: number } | null>(null);

  const isHorizontal = $derived(orientation === "horizontal");

  function onResizePointerDown(e: PointerEvent) {
    e.preventDefault();
    dragStart = { pos: isHorizontal ? e.clientX : e.clientY, startSize: currentSize };
    (e.target as HTMLElement).setPointerCapture?.(e.pointerId);
  }

  function onResizePointerMove(e: PointerEvent) {
    if (!dragStart) return;
    const current = isHorizontal ? e.clientX : e.clientY;
    const maxDim = isHorizontal ? window.innerWidth : window.innerHeight;
    const delta = fixedSide === "first" ? current - dragStart.pos : dragStart.pos - current;
    const maxSize = maxDim * 0.8;
    fixedSize = Math.max(200, Math.min(maxSize, dragStart.startSize + delta));
  }

  function onResizePointerUp(e: PointerEvent) {
    if (dragStart) (e.target as HTMLElement).releasePointerCapture?.(e.pointerId);
    dragStart = null;
  }

  const handleClass = $derived(
    isHorizontal
      ? "w-2 shrink-0 cursor-col-resize touch-none bg-base-300 hover:bg-primary/30 transition-colors flex items-center justify-center select-none"
      : "h-2 shrink-0 cursor-row-resize touch-none bg-base-300 hover:bg-primary/30 transition-colors flex items-center justify-center select-none",
  );

  const handlePip = $derived(
    isHorizontal
      ? "w-0.5 h-8 rounded-full bg-base-content/25"
      : "h-0.5 w-8 rounded-full bg-base-content/25",
  );

  const fixedStyle = $derived(
    isHorizontal
      ? `width: ${currentSize}px; max-width: calc(100% - 200px)`
      : `height: ${currentSize}px; max-height: calc(100% - 200px)`,
  );
</script>

<div
  class="{isHorizontal ? 'flex' : 'flex flex-col'} min-h-0 w-full overflow-hidden rounded-lg border border-base-content/20 {className}"
>
  {#if fixedSide === "first"}
    <div
      class="shrink-0 min-h-0 overflow-hidden flex flex-col {isHorizontal ? '' : 'min-w-0'}"
      style={fixedStyle}
    >
      {@render first()}
    </div>

    {#if secondOpen}
      <div
        class={handleClass}
        role="separator"
        aria-label="Drag to resize"
        aria-orientation={isHorizontal ? "vertical" : "horizontal"}
        onpointerdown={onResizePointerDown}
        onpointermove={onResizePointerMove}
        onpointerup={onResizePointerUp}
      >
        <div class={handlePip}></div>
      </div>

      <div class="min-w-0 flex-1 min-h-0 overflow-hidden">
        {@render second()}
      </div>
    {/if}
  {:else}
    <div class="min-w-0 flex-1 min-h-0 overflow-hidden">
      {@render first()}
    </div>

    {#if secondOpen}
      <div
        class={handleClass}
        role="separator"
        aria-label="Drag to resize"
        aria-orientation={isHorizontal ? "vertical" : "horizontal"}
        onpointerdown={onResizePointerDown}
        onpointermove={onResizePointerMove}
        onpointerup={onResizePointerUp}
      >
        <div class={handlePip}></div>
      </div>

      <div
        class="shrink-0 min-h-0 overflow-hidden flex flex-col {isHorizontal ? '' : 'min-w-0'}"
        style={fixedStyle}
      >
        {@render second()}
      </div>
    {/if}
  {/if}
</div>
