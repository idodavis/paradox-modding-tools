<script lang="ts">
  // TODO: Look into bits-ui Tabs component for better implementation.
  let {
    tabGroup = "",
    label = "",
    selected = false,
    class: tabClass = "",
    contentClass = "",
    onclick,
    children,
    ...restProps
  }: {
    tabGroup?: string;
    label?: string;
    selected?: boolean;
    class?: string;
    contentClass?: string;
    onclick?: (event: MouseEvent) => void;
    children?: import("svelte").Snippet;
  } = $props();

  let inputElement = $state<HTMLInputElement | null>(null);
  let isChecked = $state(false);

  $effect(() => {
    const element = inputElement;
    if (!element) return;

    const updateChecked = () => {
      isChecked = element.checked;
    };

    // Initial state
    updateChecked();

    element.addEventListener("change", updateChecked);

    // Also listen for clicks on other tabs in the same group
    const allInputs = document.querySelectorAll<HTMLInputElement>(
      `input[type="radio"][name="${tabGroup}"]`,
    );
    allInputs.forEach((input) => {
      input.addEventListener("change", updateChecked);
    });

    return () => {
      element.removeEventListener("change", updateChecked);
      allInputs.forEach((input) => {
        input.removeEventListener("change", updateChecked);
      });
    };
  });
</script>

<!-- Tab button -->
<input
  bind:this={inputElement}
  type="radio"
  checked={selected}
  name={tabGroup}
  class="tab {tabClass}"
  aria-label={label}
  {onclick}
  {...restProps}
/>

<!-- Tab content panel - only show when checked -->
{#if isChecked}
  <div class="tab-content {contentClass}">
    {@render children?.()}
  </div>
{/if}
