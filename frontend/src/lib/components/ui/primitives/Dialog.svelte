<script lang="ts">
  import type { Snippet } from "svelte";
  import { Dialog, type WithoutChild } from "bits-ui";

  type Size = "sm" | "md" | "lg" | "xl" | "2xl" | "fullscreen";

  type Props = Dialog.RootProps & {
    triggerDialog?: Snippet;
    closeDialog?: Snippet;
    title: Snippet;
    description: Snippet;
    /** sm | md (default) | lg | xl | fullscreen */
    size?: Size;
    contentProps?: WithoutChild<Dialog.ContentProps>;
    overlayProps?: WithoutChild<Dialog.OverlayProps>;
  };

  let {
    open = $bindable(false),
    children,
    triggerDialog,
    closeDialog,
    size = "md",
    contentProps = {},
    overlayProps,
    title,
    description,
    ...restProps
  }: Props = $props();

  const baseModal =
    "fixed left-1/2 top-1/2 -translate-x-1/2 -translate-y-1/2 z-50 overflow-auto rounded-box border border-base-300 bg-base-100 shadow-2xl px-4 w-full";
  const sizeClasses: Record<Size, string> = {
    sm: `${baseModal} max-h-[min(90vh,24rem)] max-w-sm`,
    md: `${baseModal} max-h-[min(90vh,28rem)] max-w-md`,
    lg: `${baseModal} max-h-[min(90vh,32rem)] max-w-lg`,
    xl: `${baseModal} max-h-[min(90vh,36rem)] max-w-xl`,
    "2xl": `${baseModal} max-h-[min(95vh,42rem)] max-w-3xl`,
    fullscreen: "fixed inset-0 z-50 w-full h-full max-w-none max-h-none rounded-none border-0 p-6",
  };
  const contentClass = $derived(sizeClasses[size]);
  const mergedContentProps = $derived({
    ...contentProps,
    class: [contentClass, contentProps.class].filter(Boolean).join(" "),
  });
</script>

<div class="fixed">
  <Dialog.Root bind:open {...restProps}>
    <Dialog.Trigger>
      {@render triggerDialog?.()}
    </Dialog.Trigger>
    <Dialog.Portal>
      <Dialog.Overlay {...overlayProps} />
      <Dialog.Content {...mergedContentProps}>
        <Dialog.Title>
          {@render title()}
        </Dialog.Title>
        <Dialog.Description>
          {@render description()}
        </Dialog.Description>
        {@render children?.()}
        <Dialog.Close>
          {@render closeDialog?.()}
        </Dialog.Close>
      </Dialog.Content>
    </Dialog.Portal>
  </Dialog.Root>
</div>
