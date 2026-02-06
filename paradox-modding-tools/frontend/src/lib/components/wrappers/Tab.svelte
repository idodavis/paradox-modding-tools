<script lang="ts">
  let {
    tabGroup = "",
    label = "",
    selected = false,
    class: tabClass = "",
    contentClass = "",
    children,
    ...restProps
  }: {
    tabGroup?: string;
    label?: string;
    selected?: boolean;
    class?: string;
    contentClass?: string;
    children?: import("svelte").Snippet;
  } = $props();
</script>

<!-- Tab button -->
<input
  type="radio"
  checked={selected}
  name={tabGroup}
  class="tab {tabClass}"
  aria-label={label}
  {...restProps}
/>

<!-- Tab content panel -->
<div class="tab-content {contentClass}">
  {@render children?.()}
</div>
