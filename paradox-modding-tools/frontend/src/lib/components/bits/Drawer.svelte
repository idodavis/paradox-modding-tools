<script lang="ts">
  import type { Snippet } from "svelte";
  import { Dialog, type WithoutChild } from "bits-ui";

  type Side = "left" | "right" | "top" | "bottom";

  let {
    open = $bindable(false),
    side = "right",
    resizable = true,
    defaultSize = 520,
    size = $bindable<number | undefined>(undefined),
    children,
    closeSnippet,
    titleSnippet,
    contentClass = "",
    overlayProps,
  }: {
    open?: boolean;
    side?: Side;
    resizable?: boolean;
    defaultSize?: number;
    size?: number;
    children?: Snippet;
    closeSnippet?: Snippet;
    titleSnippet?: Snippet;
    contentClass?: string;
    overlayProps?: WithoutChild<Dialog.OverlayProps>;
  } = $props();

  const currentSize = $derived(size ?? defaultSize);
  const isVertical = $derived(side === "top" || side === "bottom");

  const positionClasses: Record<Side, string> = {
    left: "left-0 top-0 bottom-0 h-full",
    right: "right-0 top-0 bottom-0 h-full",
    top: "top-0 left-0 right-0 w-full",
    bottom: "bottom-0 left-0 right-0 w-full",
  };
  const sizeStyle = $derived(
    isVertical
      ? `height: ${currentSize}px`
      : `width: ${currentSize}px; max-width: 100vw`,
  );
  const panelClass = $derived(
    `fixed z-50 bg-base-100 shadow-2xl flex flex-col overflow-hidden ${positionClasses[side]} ${contentClass}`,
  );

  let dragStart = $state<{ x: number; y: number; startSize: number } | null>(
    null,
  );
  function onResizePointerDown(e: PointerEvent) {
    if (!resizable) return;
    e.preventDefault();
    dragStart = {
      x: e.clientX,
      y: e.clientY,
      startSize: currentSize,
    };
    (e.target as HTMLElement).setPointerCapture?.(e.pointerId);
  }
  function onResizePointerMove(e: PointerEvent) {
    if (!dragStart) return;
    const delta = isVertical
      ? e.clientY - dragStart.y
      : e.clientX - dragStart.x;
    const sign = side === "right" || side === "bottom" ? -1 : 1;
    const next = Math.max(
      200,
      Math.min(900, dragStart.startSize + delta * sign),
    );
    size = next;
  }
  function onResizePointerUp(e: PointerEvent) {
    if (dragStart)
      (e.target as HTMLElement).releasePointerCapture?.(e.pointerId);
    dragStart = null;
  }

  // Resize handle on the inner edge: right drawer → left, left → right, top → bottom, bottom → top
  const handleClass = $derived(
    resizable
      ? isVertical
        ? `absolute left-0 right-0 h-3 cursor-row-resize touch-none flex items-center justify-center bg-base-300 hover:bg-primary/30 transition-colors ${side === "top" ? "bottom-0" : "top-0"}`
        : `absolute top-0 bottom-0 w-3 cursor-col-resize touch-none flex items-center justify-center bg-base-300 hover:bg-primary/30 transition-colors ${side === "left" ? "right-0" : "left-0"}`
      : "",
  );
</script>

<Dialog.Root bind:open>
  <Dialog.Portal>
    <Dialog.Overlay class="fixed inset-0 z-40 bg-black/50" {...overlayProps} />
    <Dialog.Content
      class="drawer-panel {panelClass}"
      style={sizeStyle}
      data-side={side}
    >
      <div
        class="flex items-center justify-between gap-2 border-b border-base-content/20 px-3 py-2 bg-base-200"
      >
        {#if titleSnippet}
          <div class="font-semibold truncate">{@render titleSnippet()}</div>
        {/if}
        <div class="flex items-center gap-1 ml-auto">
          {#if closeSnippet}
            <Dialog.Close>{@render closeSnippet()}</Dialog.Close>
          {/if}
        </div>
      </div>
      <div class="min-h-0 flex-1 overflow-auto">
        {@render children?.()}
      </div>
      {#if resizable && handleClass}
        <div
          class={handleClass}
          role="separator"
          aria-label="Drag to resize"
          aria-orientation={isVertical ? "horizontal" : "vertical"}
          onpointerdown={onResizePointerDown}
          onpointermove={onResizePointerMove}
          onpointerup={onResizePointerUp}
          onpointercancel={onResizePointerUp}
        >
          <span
            class="rounded-full bg-base-content/30 pointer-events-none {isVertical
              ? 'w-12 h-1'
              : 'w-1 h-8'}"
            aria-hidden="true"
          ></span>
        </div>
      {/if}
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

<style>
  :global(.drawer-panel[data-side="right"]) {
    animation: drawer-slide-right 0.2s ease-out;
  }
  :global(.drawer-panel[data-side="left"]) {
    animation: drawer-slide-left 0.2s ease-out;
  }
  :global(.drawer-panel[data-side="top"]) {
    animation: drawer-slide-top 0.2s ease-out;
  }
  :global(.drawer-panel[data-side="bottom"]) {
    animation: drawer-slide-bottom 0.2s ease-out;
  }
  @keyframes drawer-slide-right {
    from {
      transform: translateX(100%);
    }
    to {
      transform: translateX(0);
    }
  }
  @keyframes drawer-slide-left {
    from {
      transform: translateX(-100%);
    }
    to {
      transform: translateX(0);
    }
  }
  @keyframes drawer-slide-top {
    from {
      transform: translateY(-100%);
    }
    to {
      transform: translateY(0);
    }
  }
  @keyframes drawer-slide-bottom {
    from {
      transform: translateY(100%);
    }
    to {
      transform: translateY(0);
    }
  }
</style>
