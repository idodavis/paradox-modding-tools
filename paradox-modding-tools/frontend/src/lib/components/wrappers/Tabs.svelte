<script lang="ts">
  let {
    class: tabsClass = "",
    children,
    ...restProps
  }: {
    class?: string;
    children?: import("svelte").Snippet;
  } = $props();
</script>

<div class="tabs {tabsClass}" {...restProps}>
  {@render children?.()}
</div>
