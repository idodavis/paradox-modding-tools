<script lang="ts">
  let {
    class: cardBodyClass = "",
    children,
    ...restProps
  }: {
    class?: string;
    children?: import("svelte").Snippet;
  } = $props();
</script>

<div class="card-body {cardBodyClass}" {...restProps}>
  {@render children?.()}
</div>
