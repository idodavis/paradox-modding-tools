<script lang="ts">
  import { CodeBlock } from "@components";
  import { CopyToClipboard } from "@services/clipboardservice";
  import { GetAttributes, GetItemDetails } from "@services/inventoryservice";
  import type { InventoryItemRow, ItemDetails as ItemDetailsType } from "@services/models";

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
  <div class="flex flex-col h-full overflow-auto p-3 space-y-3">
    <span class="badge badge-primary">{row.type}</span>
    <div>
      <p class="text-sm font-mono break-all">{row.filePath}</p>
      <p class="text-xs text-base-content/60">
        Lines {row.lineStart}–{row.lineEnd}
      </p>
    </div>
    <div class="flex gap-2">
      <button
        type="button"
        class="btn btn-sm btn-ghost"
        onclick={() => row?.key && CopyToClipboard(row.key)}>Copy Key</button
      >
      <button
        type="button"
        class="btn btn-sm btn-ghost"
        onclick={() => row?.filePath && CopyToClipboard(row.filePath)}
        >Copy Path</button
      >
    </div>
    {#if attributesTable.length > 0}
      <details class="collapse collapse-arrow rounded bg-base-200">
        <summary class="collapse-title text-sm">Attributes</summary>
        <div class="collapse-content">
          <div class="max-h-48 min-h-0 overflow-x-hidden overflow-y-auto overscroll-contain">
            <table class="table table-xs">
            <tbody>
              {#each attributesTable as attrRow}<tr
                  ><td class="font-mono text-xs">{attrRow.key}</td>
                  <td>
                    {#if attrRow.present}
                      <span class="text-success">✓</span>
                    {:else}
                      —
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
      <details class="collapse collapse-arrow rounded bg-base-200">
        <summary class="collapse-title text-sm">{title} ({items.length})</summary>
        <div class="collapse-content">
          <div class="max-h-48 min-h-0 space-y-1 overflow-x-hidden overflow-y-auto overscroll-contain">
            {#if items.length === 0}
              <p class="p-2 text-sm text-base-content/60">None</p>
            {:else}
              {#each items as ref}
                <div class="rounded bg-base-300 p-2 text-sm font-mono">
                  <span class="text-primary">{ref.key}</span>
                  <span class="text-base-content/60">({ref.type})</span>
                  <span class="text-base-content/50 block text-xs">{ref.filePath}:{ref.lineStart}</span>
                </div>
              {/each}
            {/if}
          </div>
        </div>
      </details>
    {/each}
    <details class="collapse collapse-arrow rounded bg-base-200">
      <summary class="collapse-title text-sm">Raw Text</summary>
      <div class="collapse-content overflow-hidden">
        {#if !rawText}
          <p class="p-2 text-sm text-base-content/60">Unavailable</p>
        {:else}
          <CodeBlock
            content={rawText}
            filename={fileName}
            language="hcl"
            showCopyButton={true}
            showFullScreenButton={false}
            class="max-h-[50vh] min-h-0 rounded-lg border border-base-300 overflow-auto"
          />
        {/if}
      </div>
    </details>
  </div>
{/if}
