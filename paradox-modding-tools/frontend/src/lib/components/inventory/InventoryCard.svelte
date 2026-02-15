<script lang="ts">
  import Icon from "@iconify/svelte";
  import type { InventorySummary } from "@services/models";

  let {
    inv,
    isActive = false,
    onLoad,
    onRename,
    onDelete,
  }: {
    inv: InventorySummary;
    isActive?: boolean;
    onLoad: (inv: InventorySummary) => void;
    onRename: (inv: InventorySummary) => void;
    onDelete: (inv: InventorySummary) => void;
  } = $props();
</script>

<div
  class="group rounded-lg border p-3 transition-all duration-200 {isActive
    ? 'border-primary bg-primary/5'
    : 'border-base-content/10 bg-base-100 hover:border-base-content/30 hover:shadow-sm'}"
>
  <div class="flex items-start justify-between gap-1">
    <button
      type="button"
      class="min-w-0 flex-1 truncate text-left text-sm font-semibold {isActive
        ? 'text-primary'
        : 'text-base-content group-hover:text-primary'} transition-colors"
      title={inv.name}
      onclick={() => onLoad(inv)}
    >
      {inv.name}
    </button>
    <div
      class="flex gap-1 opacity-0 group-hover:opacity-100 transition-opacity"
    >
      <button
        type="button"
        class="btn btn-ghost btn-xs btn-square hover:bg-base-content/10"
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
        class="btn btn-ghost btn-xs btn-square text-error hover:bg-error/10"
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
