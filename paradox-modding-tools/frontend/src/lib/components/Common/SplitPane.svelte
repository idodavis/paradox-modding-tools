<script lang="ts">
  import type { Snippet } from "svelte";

  let {
    left,
    right,
    rightOpen = true,
    defaultRightSize = 600,
    fixedSide = "right",
    class: className = "",
  }: {
    left: Snippet;
    right: Snippet;
    rightOpen?: boolean;
    defaultRightSize?: number;
    fixedSide?: "left" | "right";
    class?: string;
  } = $props();

  let fixedSize = $state<number | undefined>(undefined);
  const currentSize = $derived(fixedSize ?? defaultRightSize);
  let dragStart = $state<{ x: number; startSize: number } | null>(null);

  function onResizePointerDown(e: PointerEvent) {
    e.preventDefault();
    dragStart = { x: e.clientX, startSize: currentSize };
    (e.target as HTMLElement).setPointerCapture?.(e.pointerId);
  }

  function onResizePointerMove(e: PointerEvent) {
    if (!dragStart) return;
    // fixedSide="right": moving handle left grows the right panel
    // fixedSide="left":  moving handle right grows the left panel
    const delta =
      fixedSide === "left" ? e.clientX - dragStart.x : dragStart.x - e.clientX;
    const maxSize = window.innerWidth * 0.8;
    fixedSize = Math.max(200, Math.min(maxSize, dragStart.startSize + delta));
  }

  function onResizePointerUp(e: PointerEvent) {
    if (dragStart)
      (e.target as HTMLElement).releasePointerCapture?.(e.pointerId);
    dragStart = null;
  }

  const handleMarkup = `w-2 shrink-0 cursor-col-resize touch-none bg-base-300 hover:bg-primary/30 transition-colors flex items-center justify-center select-none`;
</script>

<div
  class="flex min-h-0 w-full overflow-hidden rounded-lg border border-base-content/20 {className}"
>
  {#if fixedSide === "left"}
    <!-- Fixed-width left panel -->
    <div
      class="shrink-0 min-h-0 overflow-hidden flex flex-col"
      style="width: {currentSize}px; max-width: calc(100% - 200px)"
    >
      {@render left()}
    </div>

    {#if rightOpen}
      <!-- Drag handle -->
      <div
        class={handleMarkup}
        role="separator"
        aria-label="Drag to resize"
        aria-orientation="vertical"
        onpointerdown={onResizePointerDown}
        onpointermove={onResizePointerMove}
        onpointerup={onResizePointerUp}
      >
        <div class="w-0.5 h-8 rounded-full bg-base-content/25"></div>
      </div>

      <!-- Fluid right panel -->
      <div class="min-w-0 flex-1 min-h-0 overflow-hidden">
        {@render right()}
      </div>
    {/if}
  {:else}
    <!-- Fluid left panel -->
    <div class="min-w-0 flex-1 min-h-0 overflow-hidden">
      {@render left()}
    </div>

    {#if rightOpen}
      <!-- Drag handle -->
      <div
        class={handleMarkup}
        role="separator"
        aria-label="Drag to resize"
        aria-orientation="vertical"
        onpointerdown={onResizePointerDown}
        onpointermove={onResizePointerMove}
        onpointerup={onResizePointerUp}
      >
        <div class="w-0.5 h-8 rounded-full bg-base-content/25"></div>
      </div>

      <!-- Fixed-width right panel -->
      <div
        class="shrink-0 min-h-0 overflow-hidden flex flex-col"
        style="width: {currentSize}px; max-width: calc(100% - 200px)"
      >
        {@render right()}
      </div>
    {/if}
  {/if}
</div>
