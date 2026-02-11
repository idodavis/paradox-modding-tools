<script lang="ts">
  import {
    Card,
    CardBody,
    FileSelector,
    Grid,
    Drawer,
    MultiSelect,
  } from "@components";
  import { game } from "@stores/app";
  import {
    GetSupportedTypes,
    ExtractInventory,
  } from "@services/inventoryservice";
  import type { InventoryItem } from "@services/models";
  import ItemDetails from "../components/inventory/ItemDetails.svelte";
  import ExportImportDialog from "../components/inventory/ExportImportDialog.svelte";

  let files = $state<string[]>([]);
  let selectedTypes = $state<string[]>([]);
  let supportedTypes = $state<string[]>([]);
  let hasExtraction = $state(false);
  let loading = $state(false);
  let extractionErrors = $state<string[]>([]);
  let allItems = $state<InventoryItem[]>([]);
  let selectedItem = $state<InventoryItem | null>(null);
  let itemDetailsOpen = $state(false);
  let showExportImport = $state(false);
  let extractionPromise = $state<
    (Promise<unknown> & { cancel?: () => void }) | null
  >(null);

  $effect(() => {
    if (!itemDetailsOpen) selectedItem = null;
  });

  $effect(() => {
    GetSupportedTypes($game).then((t) => (supportedTypes = t));
  });

  function cancelExtraction() {
    if (extractionPromise?.cancel) {
      extractionPromise.cancel();
    }
    extractionPromise = null;
  }

  async function doExtract() {
    if (!files.length || !selectedTypes.length) return;
    loading = true;
    hasExtraction = false;
    extractionErrors = [];
    allItems = [];
    try {
      extractionPromise = ExtractInventory($game, files, selectedTypes);
      const items = await extractionPromise;
      hasExtraction = true;
      allItems = (items ?? []) as InventoryItem[];
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      if (!msg.toLowerCase().includes("cancel")) {
        extractionErrors = [msg];
      }
    } finally {
      loading = false;
      extractionPromise = null;
    }
  }

  function clearAll() {
    hasExtraction = false;
    extractionErrors = [];
    allItems = [];
    selectedItem = null;
    itemDetailsOpen = false;
  }

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
      valueGetter: (p: { data?: InventoryItem }) =>
        `${p.data?.lineStart ?? 0} - ${p.data?.lineEnd ?? 0}`,
      filter: false,
      sortable: false,
      flex: 1,
    },
    {
      headerName: "References",
      flex: 1,
      valueGetter: (p: { data?: InventoryItem }) =>
        p.data?.references?.length ?? 0,
      filter: true,
    },
    {
      headerName: "Referrers",
      flex: 1,
      valueGetter: (p: { data?: InventoryItem }) =>
        p.data?.referrers?.length ?? 0,
      filter: true,
    },
  ]);
</script>

<div class="p-4">
  <Card class="mb-4">
    <CardBody>
      <h2 class="text-lg font-semibold mb-4">Object Inventory</h2>
      <FileSelector
        legend="Files / folders"
        mode="filesAndFolders"
        dialogTitle="Files or folders"
        fileBtnText="Files"
        folderBtnText="Folders"
        placeholder="Paths…"
        initialValue={files}
        onPathsChange={(p) => (files = p)}
      />
      <div class="my-4">
        <span class="label text-base-content/90 block mb-1">Object types</span>
        <div class="flex flex-wrap items-start gap-2">
          <MultiSelect
            items={supportedTypes}
            bind:selected={selectedTypes}
            placeholder="Types…"
            checkboxColor="checkbox-success"
            disabled={supportedTypes.length === 0}
            size="w-72"
          />

          <button
            type="button"
            class="btn btn-ghost btn-sm mt-1"
            disabled={supportedTypes.length === 0}
            onclick={() => (selectedTypes = supportedTypes)}
          >
            Select all
          </button>
          <button
            type="button"
            class="btn btn-ghost btn-sm mt-1"
            disabled={supportedTypes.length === 0 || !selectedTypes.length}
            onclick={() => (selectedTypes = [])}
          >
            Clear
          </button>
        </div>
      </div>
      <div class="flex flex-wrap gap-2">
        {#if !loading}
          <button
            type="button"
            class="btn btn-primary"
            disabled={!files.length || !selectedTypes.length}
            onclick={doExtract}>Extract</button
          >
        {:else}
          <button type="button" class="btn btn-error" onclick={cancelExtraction}
            >Cancel</button
          >
        {/if}
        <button
          type="button"
          class="btn btn-ghost"
          disabled={loading}
          onclick={() => (showExportImport = true)}>Export / Import</button
        >
        <button
          type="button"
          class="btn btn-ghost btn-error"
          disabled={loading}
          onclick={clearAll}>Clear</button
        >
      </div>
    </CardBody>
  </Card>

  {#if extractionErrors.length > 0}
    <div class="alert alert-warning mb-4">
      <span class="font-medium">Errors ({extractionErrors.length}):</span>
      <ul class="list-disc pl-5 text-sm max-h-24 overflow-auto">
        {#each extractionErrors as err}<li>{err}</li>{/each}
      </ul>
    </div>
  {/if}

  {#if hasExtraction}
    <Grid
      {columnDefs}
      rowData={allItems}
      gridOptions={{
        pagination: true,
        paginationPageSize: 20,
        rowSelection: {
          mode: "singleRow",
        },
        onRowClicked: (e) => {
          if (e?.data) {
            selectedItem = e.data;
            itemDetailsOpen = true;
          }
        },
      }}
      className="h-100 w-full"
    />
  {:else}
    <div
      class="flex items-center justify-center py-16 text-center text-base-content/60"
    >
      <p>No inventory. Select paths and types, then Extract.</p>
    </div>
  {/if}
</div>

<Drawer
  bind:open={itemDetailsOpen}
  side="right"
  defaultSize={580}
  contentClass="max-w-[90vw]"
>
  {#snippet titleSnippet()}Item Details{/snippet}
  {#snippet closeSnippet()}<button
      type="button"
      class="btn btn-sm btn-ghost"
      onclick={() => (itemDetailsOpen = false)}>Close</button
    >{/snippet}
  <ItemDetails item={selectedItem} game={$game} />
</Drawer>

{#if showExportImport}
  <ExportImportDialog bind:open={showExportImport} {hasExtraction} />
{/if}
