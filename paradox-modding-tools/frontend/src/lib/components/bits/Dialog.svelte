<script lang="ts">
  import type { Snippet } from "svelte";
  import { Dialog, type WithoutChild } from "bits-ui";

  type Props = Dialog.RootProps & {
    triggerDialog?: Snippet;
    closeDialog?: Snippet;
    title: Snippet;
    description: Snippet;
    contentProps?: WithoutChild<Dialog.ContentProps>;
    overlayProps?: WithoutChild<Dialog.OverlayProps>;
  };

  let {
    open = $bindable(false),
    children,
    triggerDialog,
    closeDialog,
    contentProps,
    overlayProps,
    title,
    description,
    ...restProps
  }: Props = $props();
</script>

<Dialog.Root bind:open {...restProps}>
  <Dialog.Trigger>
    {@render triggerDialog?.()}
  </Dialog.Trigger>
  <Dialog.Portal>
    <Dialog.Overlay {...overlayProps} />
    <Dialog.Content {...contentProps}>
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
