<script lang="ts">
  import { Card, CardBody, FileSelector, Grid, Drawer, MultiSelect } from "@components";
  import { game } from "@stores/app.svelte";
  import { ItemDetails, InventoryCard, InventoryNameModal } from "@components";
  import { inventoryStore, setInventoryStore } from "@stores/inventory.svelte";
  import type { InventoryItemRow } from "@services/models";
  import type { ValueGetterParams } from "ag-grid-community";

  const store = inventoryStore;
  setInventoryStore(store);

  $effect(() => {
    $game;
    store.refresh();
  });

  const columnDefs = $derived([
    {
      field: "type",
      headerName: "Type",
      filter: true,
      sortable: true,
      flex: 1,
    },
    { field: "key", headerName: "Key", filter: true, sortable: true, flex: 2 },
    {
      field: "filePath",
      headerName: "File",
      filter: true,
      sortable: true,
      flex: 3,
    },
    {
      field: "lines",
      headerName: "Lines",
      valueGetter: (p: ValueGetterParams<InventoryItemRow>) =>
        `${p.data?.lineStart ?? 0} - ${p.data?.lineEnd ?? 0}`,
      filter: false,
      sortable: false,
      flex: 1,
    },
    {
      field: "referencesCount",
      headerName: "References",
      flex: 1,
      filter: true,
    },
    { field: "referrersCount", headerName: "Referrers", flex: 1, filter: true },
  ]);

  $effect(() => {
    if (!store.itemDetailsOpen) store.selectedRow = null;
  });
</script>

<div class="relative p-4 max-w-full min-w-0">
  <Card class="mb-4">
    <CardBody>
      <div class="flex flex-col gap-6">
        <div class="space-y-4">
          <FileSelector
            legend="Folder to Extract"
            mode="folder"
            dialogTitle="Select a folder"
            btnText="Folder"
            btnColor="btn-secondary"
            placeholder="Folder containing the inventory (e.g. mod folder, game files)"
            initialValue={store.file}
            onPathChange={(p) => (store.file = p)}
          />
          <div>
            <span class="label text-base-content/90 mb-1 block font-medium">Object types</span>
            <div class="flex flex-wrap items-center gap-2">
              <MultiSelect
                items={store.supportedTypes}
                bind:selected={store.selectedTypes}
                placeholder="Select types…"
                checkboxColor="checkbox-success"
                disabled={store.typesDisabled}
                size="w-full sm:w-72"
              />
              <div class="join">
                <button
                  type="button"
                  class="join-item btn btn-soft btn-sm"
                  disabled={store.typesDisabled}
                  onclick={() => (store.selectedTypes = store.supportedTypes)}>All</button
                >
                <button
                  type="button"
                  class="join-item btn btn-soft btn-sm"
                  disabled={store.typesDisabled || !store.selectedTypes.length}
                  onclick={() => (store.selectedTypes = [])}>None</button
                >
              </div>
            </div>
            <p class="mt-2 text-xs text-base-content/60">
              Note: gfx/, gui/, and music/ types are not currently supported.
            </p>
          </div>
        </div>

        {#if store.savedInventories.length > 0}
          <div
            class="flex flex-col min-w-0 overflow-hidden bg-base-200/50 rounded-xl border border-base-content/10 p-3"
          >
            <div class="flex justify-between items-center mb-2 px-1">
              <span class="text-sm font-semibold text-base-content/80">Saved Inventories</span>
              <span class="badge badge-sm badge-neutral">{store.savedInventories.length}</span>
            </div>
            <div class="flex flex-row gap-3 overflow-x-auto pb-2 custom-scrollbar">
              {#each store.savedInventories as inv}
                <div class="w-72 flex-none">
                  <InventoryCard
                    {inv}
                    isActive={store.currentInventoryId === inv.id}
                    onLoad={(i) => store.loadInventory(i)}
                    onRename={() => store.openModal("rename", inv)}
                    onDelete={(i) => store.handleDelete(i)}
                  />
                </div>
              {/each}
            </div>
          </div>
        {/if}
      </div>

      <div class="flex flex-wrap gap-2 mt-6 pt-4 border-t border-base-content/10">
        <button
          type="button"
          class="btn {store.loading ? 'btn-soft btn-error' : 'btn-primary'} min-w-[120px]"
          disabled={store.loading ? false : store.extractDisabled}
          onclick={store.loading
            ? () => {
                store.cancelExtraction();
              }
            : () => store.doExtract()}
        >
          {store.loading ? "Cancel" : "Extract"}
        </button>
        <button
          type="button"
          class="btn btn-ghost text-error hover:bg-error/10"
          disabled={store.loading}
          onclick={() => store.clearAll()}>Clear Results</button
        >
      </div>
    </CardBody>
  </Card>

  <div class="min-w-0">
    {#if store.extractionErrors.length > 0}
      <div class="alert alert-warning mb-4">
        <span class="font-medium">Errors ({store.extractionErrors.length}):</span>
        <ul class="list-disc pl-5 text-sm max-h-24 overflow-auto">
          {#each store.extractionErrors as err}<li>{err}</li>{/each}
        </ul>
      </div>
    {/if}

    {#if store.hasExtraction}
      <Card class="border border-base-300 shadow-sm">
        <CardBody class="p-4">
          <div
            class="mb-3 flex items-center justify-between bg-base-200/50 p-2 rounded-lg border border-base-content/5"
          >
            <span class="text-sm font-medium text-base-content/80 ml-2">
              {store.allItems.length.toLocaleString()} items found
            </span>
            {#if store.isCurrentTemp}
              <button type="button" class="btn btn-primary btn-sm" onclick={() => store.openModal("save")}>
                Save Inventory
              </button>
            {/if}
          </div>
          <Grid
            {columnDefs}
            rowData={store.allItems}
            gridOptions={{
              pagination: true,
              paginationPageSize: 20,
              onRowClicked: (e) => {
                if (e?.data) {
                  store.selectedRow = e.data;
                  store.itemDetailsOpen = true;
                }
              },
            }}
            className="h-[min(28rem,60vh)] w-full rounded border border-base-200"
          />
        </CardBody>
      </Card>
    {:else}
      <div class="flex items-center justify-center py-16 text-center text-base-content/60">
        <p>No inventory. Select path and types, then Extract.</p>
      </div>
    {/if}
  </div>
</div>

<Drawer bind:open={store.itemDetailsOpen} side="right" defaultSize={580} contentClass="max-w-[90vw] shadow-2xl">
  {#snippet titleSnippet()}Item Details{/snippet}
  {#snippet closeSnippet()}
    <button type="button" class="btn btn-sm btn-ghost" onclick={() => (store.itemDetailsOpen = false)}>Close</button>
  {/snippet}
  <ItemDetails
    inventoryId={store.currentInventoryId}
    itemType={store.selectedRow?.type ?? null}
    itemKey={store.selectedRow?.key ?? null}
    row={store.selectedRow}
    game={$game}
  />
</Drawer>

<InventoryNameModal
  bind:open={store.nameModal.open}
  mode={store.nameModal.mode}
  initialName={store.nameModalInitialName}
  onsave={(name) => store.handleNameModalSave(name)}
/>
