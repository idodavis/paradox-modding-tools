<script lang="ts">
  import { CodeBlock } from "@components";
  import { CopyToClipboard } from "@services/clipboardservice";
  import { GetAttributes, GetItemDetails } from "@services/inventoryservice";
  import type {
    InventoryItemRow,
    ItemDetails as ItemDetailsType,
  } from "@services/models";

  let {
    inventoryId = null,
    itemType = null,
    itemKey = null,
    row = null,
    game = "CK3",
  }: {
    inventoryId: string | null;
    itemType: string | null;
    itemKey: string | null;
    row: InventoryItemRow | null;
    game?: string;
  } = $props();

  let details = $state<ItemDetailsType | null>(null);

  $effect(() => {
    if (!inventoryId || !itemType || !itemKey) {
      details = null;
      return;
    }
    GetItemDetails(inventoryId, itemType, itemKey).then((d) => {
      details = d ?? null;
    });
  });

  let itemAttributes = $state<string[]>([]);
  $effect(() => {
    if (!game || !itemType) {
      itemAttributes = [];
      return;
    }
    GetAttributes(game, itemType).then((a) => {
      itemAttributes = a ?? [];
    });
  });

  const presentSet = $derived.by(() => {
    const attrs = details?.attributes;
    if (!attrs || typeof attrs !== "object") return new Set<string>();
    return new Set(Object.keys(attrs).filter((k) => attrs[k]));
  });
  const attributesTable = $derived(
    itemAttributes.map((key) => ({ key, present: presentSet.has(key) })),
  );
  const rawText = $derived(details?.rawText ?? "");
  const fileName = $derived(row?.filePath?.split(/[/\\]/).pop() ?? "");
  const references = $derived(details?.references ?? []);
  const referrers = $derived(details?.referrers ?? []);
  const refSections = $derived([
    { title: "Referrers", items: referrers },
    { title: "References", items: references },
  ]);
</script>

{#if !row}
  <p class="text-base-content/60 p-4">No item selected</p>
{:else}
  <div
    class="flex flex-col h-full overflow-y-auto overflow-x-hidden p-4 space-y-4 pr-6"
  >
    <div class="flex items-center justify-between gap-2">
      <span class="badge badge-primary badge-lg">{row.type}</span>
      <span class="text-sm text-base-content/80">{row.key}</span>
      <span class="text-xs font-mono text-base-content/50"
        >Lines {row.lineStart}–{row.lineEnd}</span
      >
    </div>

    <div class="p-3 bg-base-200/50 rounded-lg border border-base-content/10">
      <p class="text-sm font-mono break-all select-all">{row.filePath}</p>
    </div>

    <div class="flex gap-2">
      <button
        type="button"
        class="btn btn-sm btn-soft flex-1"
        onclick={() => row?.key && CopyToClipboard(row.key)}>Copy Key</button
      >
      <button
        type="button"
        class="btn btn-sm btn-soft flex-1"
        onclick={() => row?.filePath && CopyToClipboard(row.filePath)}
        >Copy Path</button
      >
    </div>
    {#if attributesTable.length > 0}
      <details
        class="collapse collapse-arrow rounded-lg border border-base-content/10 bg-base-100"
      >
        <summary class="collapse-title text-sm font-medium">Attributes</summary>
        <div class="collapse-content !pb-2">
          <div class="max-h-60 overflow-y-auto custom-scrollbar">
            <table class="table table-xs w-full">
              <tbody>
                {#each attributesTable as attrRow}<tr
                    ><td class="font-mono text-xs">{attrRow.key}</td>
                    <td>
                      {#if attrRow.present}
                        <span class="text-success">✓</span>
                      {:else}
                        <span class="opacity-30">—</span>
                      {/if}
                    </td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        </div>
      </details>
    {/if}
    {#each refSections as { title, items }}
      <details
        class="collapse collapse-arrow rounded-lg border border-base-content/10 bg-base-100"
      >
        <summary class="collapse-title text-sm font-medium"
          >{title} <span class="opacity-60">({items.length})</span></summary
        >
        <div class="collapse-content !pb-2">
          <div class="space-y-2 max-h-60 overflow-y-auto custom-scrollbar pr-2">
            {#if items.length === 0}
              <p class="text-sm text-base-content/50 italic">None found</p>
            {:else}
              {#each items as ref}
                <div
                  class="rounded bg-base-200/50 p-2 text-sm font-mono border border-base-content/5 hover:border-base-content/20 transition-colors"
                >
                  <div class="flex justify-between items-baseline">
                    <span class="text-primary font-semibold">{ref.key}</span>
                    <span
                      class="text-xs text-base-content/60 bg-base-300 px-1.5 py-0.5 rounded"
                      >{ref.type}</span
                    >
                  </div>
                  <span
                    class="text-base-content/50 block text-xs mt-1 truncate"
                    title="{ref.filePath}:{ref.lineStart}"
                    >{ref.filePath}:{ref.lineStart}</span
                  >
                </div>
              {/each}
            {/if}
          </div>
        </div>
      </details>
    {/each}
    <details
      class="collapse collapse-arrow rounded-lg border border-base-content/10 bg-base-100"
    >
      <summary class="collapse-title text-sm font-medium">Raw Text</summary>
      <div class="collapse-content !pb-0 !px-0">
        {#if !rawText}
          <p class="p-4 text-sm text-base-content/60">Unavailable</p>
        {:else}
          <CodeBlock
            content={rawText}
            filename={fileName}
            showCopyButton={true}
            showFullScreenButton={true}
            class="h-96 min-h-0 border-t border-base-content/10 rounded-none"
          />
        {/if}
      </div>
    </details>
  </div>
{/if}
