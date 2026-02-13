<script lang="ts">
  import Icon from "@iconify/svelte";
  import type { InventorySummary } from "@services/models";

  let {
    inv,
    onLoad,
    onRename,
    onDelete,
  }: {
    inv: InventorySummary;
    onLoad: (inv: InventorySummary) => void;
    onRename: (inv: InventorySummary) => void;
    onDelete: (inv: InventorySummary) => void;
  } = $props();
</script>

<div class="rounded-lg border border-base-300 bg-base-200 p-2.5 shadow-sm">
  <div class="flex items-start justify-between gap-1">
    <button
      type="button"
      class="min-w-0 flex-1 truncate text-left text-sm font-medium hover:text-primary"
      title={inv.name}
      onclick={() => onLoad(inv)}
    >
      {inv.name}
    </button>
    <div class="flex gap-0.5">
      <button
        type="button"
        class="btn btn-ghost btn-xs btn-square"
        title="Rename"
        onclick={(e) => {
          e.stopPropagation();
          onRename(inv);
        }}
      >
        <Icon icon="mdi:pencil" class="size-3.5" />
      </button>
      <button
        type="button"
        class="btn btn-ghost btn-xs btn-square text-error"
        title="Delete"
        onclick={(e) => {
          e.stopPropagation();
          onDelete(inv);
        }}
      >
        <Icon icon="mdi:trash-can-outline" class="size-3.5" />
      </button>
    </div>
  </div>
  <div class="mt-1 flex flex-wrap items-center gap-1.5">
    <span class="badge badge-outline badge-sm">{inv.game}</span>
    <span class="text-xs text-base-content/60">{inv.totalCount ?? 0} items</span
    >
  </div>
  <p class="mt-0.5 text-xs text-base-content/50">{inv.createdAt}</p>
  <button
    type="button"
    class="btn btn-primary btn-xs mt-2 w-full"
    onclick={() => onLoad(inv)}
  >
    Load
  </button>
</div>
