<script lang="ts">
  import { toasts } from "@stores/toast.svelte"; // or wherever the store lives
</script>

{#each $toasts as item (item.id)}
  <div class="toast z-100 {item.placement}">
    <div class="alert {item.type}">
      {#if item.contentHtml}
        {@html item.contentHtml}
      {:else}
        <span>{item.message}</span>
      {/if}
    </div>
  </div>
{/each}
