<script lang="ts">
  import { CodeBlock } from "@components";
  import { CopyToClipboard } from "@services/clipboardservice";
  import { GetAttributes } from "@services/inventoryservice";
  import type { InventoryItem } from "@services/internal/inventory/models";

  let {
    item = null,
    game = "CK3",
  }: { item: InventoryItem | null; game?: string } = $props();

  let itemAttributes = $state<string[]>([]);
  $effect(() => {
    if (!game || !item?.type) {
      itemAttributes = [];
      return;
    }
    GetAttributes(game, item.type).then((a) => {
      itemAttributes = a ?? [];
    });
  });

  const presentSet = $derived.by(() => {
    const attrs = item?.attributes;
    if (!attrs || typeof attrs !== "object") return new Set<string>();
    return new Set(Object.keys(attrs).filter((k) => attrs[k]));
  });
  const attributesTable = $derived(
    itemAttributes.map((key) => ({ key, present: presentSet.has(key) })),
  );
  const rawText = $derived(item?.rawText ?? "");
  const fileName = $derived(item?.filePath?.split(/[/\\]/).pop() ?? "");
  const references = $derived(item?.references ?? []);
</script>

{#if !item}
  <p class="text-base-content/60 p-4">No item selected</p>
{:else}
  <div class="flex flex-col h-full overflow-auto p-3 space-y-3">
    <span class="badge badge-primary">{item.type}</span>
    <div>
      <p class="text-sm font-mono break-all">{item.filePath}</p>
      <p class="text-xs text-base-content/60">
        Lines {item.lineStart}–{item.lineEnd}
      </p>
    </div>
    <div class="flex gap-2">
      <button
        type="button"
        class="btn btn-sm btn-ghost"
        onclick={() => item?.key && CopyToClipboard(item.key)}>Copy Key</button
      >
      <button
        type="button"
        class="btn btn-sm btn-ghost"
        onclick={() => item?.filePath && CopyToClipboard(item.filePath)}
        >Copy Path</button
      >
    </div>
    {#if attributesTable.length > 0}
      <details class="collapse collapse-arrow bg-base-200 rounded">
        <summary class="collapse-title text-sm">Attributes</summary>
        <div class="collapse-content">
          <table class="table table-xs">
            <tbody>
              {#each attributesTable as row}<tr
                  ><td class="font-mono text-xs">{row.key}</td>
                  <td>
                    {#if row.present}
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
      </details>
    {/if}
    <details class="collapse collapse-arrow bg-base-200 rounded">
      <summary class="collapse-title text-sm">
        References ({references.length})
      </summary>
      <div class="collapse-content max-h-48 overflow-auto space-y-1">
        {#if references.length === 0}
          <p class="text-sm text-base-content/60 p-2">None</p>
        {:else}
          {#each references as ref}
            <div class="p-2 rounded text-sm font-mono bg-base-300">
              <span class="text-primary">{ref.targetKey}</span>
              <span class="text-base-content/60">({ref.targetType})</span>
              <span class="text-base-content/50 block text-xs"
                >{ref.sourceFile}:{ref.sourceLine}</span
              >
            </div>
          {/each}
        {/if}
      </div>
    </details>
    <details class="collapse collapse-arrow bg-base-200 rounded">
      <summary class="collapse-title text-sm">Raw Text</summary>
      <div class="collapse-content overflow-hidden">
        {#if !rawText}
          <p class="text-sm text-base-content/60 p-2">Unavailable</p>
        {:else}
          <div
            class="flex flex-col max-h-[50vh] min-h-0 rounded-lg border border-base-300 overflow-hidden"
          >
            <CodeBlock
              content={rawText}
              filename={fileName}
              language="hcl"
              showCopyButton={true}
              showFullScreenButton={false}
              class="min-h-0 flex-1"
            />
          </div>
        {/if}
      </div>
    </details>
  </div>
{/if}
