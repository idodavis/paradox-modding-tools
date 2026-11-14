/** Splitter panes sized in pixels: their size kept while their page is hidden, and saved. */
import { useResizeObserver } from "@vueuse/core";
import { onActivated, type ShallowRef } from "vue";

/** A Splitter is a USplitter's template ref. Its element is always there, though its type leaves it out. */
type Splitter = {
  $el?: HTMLElement;
  panelsRef: ({ resize(size: number): void; isCollapsed: boolean } | null | undefined)[];
};

/**
 * useKeptPixelSize keeps the size of the given pane of a splitter while its page is hidden. It returns the function
 * for the splitter's layout event, which reports whether it kept the size. When the page shows again with the same
 * panes, the pane gets its size back once the splitter has measured itself; sizes reported before then are ignored.
 */
export function useKeptPixelSize(splitter: Readonly<ShallowRef<Splitter | null>>, pane: number) {
  let size = 0;
  let panes = 0;
  let returning = false;
  onActivated(() => (returning = true));
  useResizeObserver(
    () => splitter.value?.$el,
    ([entry]) => {
      if (!returning || !entry?.contentRect.width) return;
      returning = false;
      if (size && splitter.value?.panelsRef.length === panes) splitter.value.panelsRef[pane]?.resize(size);
    },
  );
  return (sizes: number[]): boolean => {
    if (returning && size) return false;
    size = sizes[pane] ?? 0;
    panes = sizes.length;
    return true;
  };
}

/**
 * useSavedPixelSize saves the size of the given pane of a splitter under the given key, and keeps it while its page
 * is hidden. The size is saved when the user lets go of the splitter's handle, unless the pane is collapsed. It
 * returns the size the pane takes when it appears, the saved one or else the given size, the function for the
 * splitter's layout event while the pane is shown, and the one for its dragging event.
 */
export function useSavedPixelSize(
  splitter: Readonly<ShallowRef<Splitter | null>>,
  pane: number,
  key: string,
  size: number,
) {
  const keepSize = useKeptPixelSize(splitter, pane);
  let kept = 0;
  return {
    // Read from storage, not a ref, so dragging the pane does not change the panes.
    size: () => Number(localStorage.getItem(key)) || size,
    keep: (sizes: number[]) => {
      if (keepSize(sizes)) kept = sizes[pane] ?? 0;
    },
    save: (_handle: number, isDragging: boolean) => {
      if (!isDragging && kept && !splitter.value?.panelsRef[pane]?.isCollapsed) {
        localStorage.setItem(key, String(Math.round(kept)));
      }
    },
  };
}
