<script lang="ts">
  let {
    class: cardClass = "",
    children,
    ...restProps
  }: {
    class?: string;
    children?: import("svelte").Snippet;
  } = $props();
</script>

<div class="card {cardClass}" {...restProps}>
  {@render children?.()}
</div>
