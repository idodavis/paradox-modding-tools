<script lang="ts">
  import {
    Card,
    CardBody,
    FileSelector,
    Grid,
    Drawer,
    MultiSelect,
  } from "@components";
  import { game } from "@stores/app.svelte";
  import * as InventoryService from "@services/inventoryservice";
  import type {
    ExtractInventoryResult,
    InventoryItemRow,
    InventorySummary,
  } from "@services/models";
  import ItemDetails from "../components/inventory/ItemDetails.svelte";
  import InventoryCard from "../components/inventory/InventoryCard.svelte";
  import ExportImportDialog from "../components/inventory/ExportImportDialog.svelte";
  import InventoryNameModal from "../components/inventory/InventoryNameModal.svelte";

  let files = $state<string[]>([]);
  let selectedTypes = $state<string[]>([]);
  let supportedTypes = $state<string[]>([]);
  let hasExtraction = $state(false);
  let loading = $state(false);
  let extractionErrors = $state<string[]>([]);
  let allItems = $state<InventoryItemRow[]>([]);
  let currentInventoryId = $state<string | null>(null);
  let currentInventoryGame = $state<string | null>(null);
  let selectedRow = $state<InventoryItemRow | null>(null);
  let savedInventories = $state<InventorySummary[]>([]);
  let isCurrentTemp = $state(false);
  let itemDetailsOpen = $state(false);
  let showExportImport = $state(false);
  let nameModal = $state<{
    open: boolean;
    mode: "save" | "rename";
    invId: string | null;
  }>({
    open: false,
    mode: "save",
    invId: null,
  });
  let extractionPromise = $state<
    (Promise<unknown> & { cancel?: () => void }) | null
  >(null);

  const typesDisabled = $derived(supportedTypes.length === 0);
  const extractDisabled = $derived(!files.length || !selectedTypes.length);

  const nameModalInitialName = $derived(
    nameModal.mode === "save"
      ? `${$game} - ${new Date().toISOString().slice(0, 10)} - ${Math.random().toString(36).slice(2, 8)}`
      : (savedInventories.find((i) => i.id === nameModal.invId)?.name ?? ""),
  );

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
      valueGetter: (p: { data?: InventoryItemRow }) =>
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
    if (!itemDetailsOpen) selectedRow = null;
  });

  $effect(() => {
    Promise.all([
      InventoryService.GetSupportedTypes($game),
      InventoryService.ListInventoriesForGame($game),
    ]).then(([types, list]) => {
      supportedTypes = types ?? [];
      savedInventories = list ?? [];
    });
  });

  $effect(() => {
    if (
      currentInventoryId &&
      currentInventoryGame !== null &&
      currentInventoryGame !== $game
    ) {
      clearAll();
    }
  });

  async function refresh() {
    const list = await InventoryService.ListInventoriesForGame($game);
    savedInventories = list ?? [];
    return list ?? [];
  }

  async function doExtract() {
    if (!files.length || !selectedTypes.length) return;
    loading = true;
    clearAll();
    try {
      extractionPromise = InventoryService.ExtractInventory(
        $game,
        files,
        selectedTypes,
      );
      const result = (await extractionPromise) as ExtractInventoryResult | null;
      if (result) {
        currentInventoryId = result.inventoryId;
        currentInventoryGame = $game;
        hasExtraction = true;
        isCurrentTemp = true;
        allItems =
          (await InventoryService.GetInventoryItems(result.inventoryId)) ?? [];
      }
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      if (!msg.toLowerCase().includes("cancel")) extractionErrors = [msg];
    } finally {
      loading = false;
      extractionPromise = null;
    }
  }

  async function loadInventory(inv: InventorySummary) {
    currentInventoryId = inv.id;
    currentInventoryGame = inv.game;
    hasExtraction = true;
    isCurrentTemp = false;
    allItems = (await InventoryService.GetInventoryItems(inv.id)) ?? [];
  }

  async function handleNameModalSave(name: string) {
    if (!nameModal.invId) return;
    if (nameModal.mode === "save") {
      await InventoryService.SaveInventory(nameModal.invId, name);
      isCurrentTemp = false;
    } else {
      await InventoryService.RenameInventory(nameModal.invId, name);
      if (currentInventoryId === nameModal.invId) {
        const idx = savedInventories.findIndex((i) => i.id === nameModal.invId);
        if (idx >= 0)
          savedInventories[idx] = { ...savedInventories[idx], name };
      }
    }
    refresh();
  }

  async function handleDelete(inv: InventorySummary) {
    await InventoryService.DeleteInventory(inv.id);
    if (currentInventoryId === inv.id) clearAll();
    refresh();
  }

  function clearAll() {
    hasExtraction = false;
    extractionErrors = [];
    allItems = [];
    currentInventoryId = null;
    currentInventoryGame = null;
    isCurrentTemp = false;
    selectedRow = null;
    itemDetailsOpen = false;
  }

  function openModal(mode: "save" | "rename", inv?: InventorySummary) {
    nameModal = {
      open: true,
      mode,
      invId: currentInventoryId ?? inv?.id ?? null,
    };
  }
</script>

<div class="relative p-4">
  <Card class="mb-4">
    <CardBody>
      <div class="flex gap-4 items-start min-w-0">
        <div class="flex-1 min-w-0 max-w-2xl">
          <FileSelector
            legend="Files / folders"
            mode="filesAndFolders"
            dialogTitle="Files or folders"
            fileBtnText="Files"
            folderBtnText="Folders"
            placeholder="Paths…"
            initialValue={files}
            onPathsChange={(p) => (files = p)}
            clearLabel="Clear"
          />
        </div>
        {#if savedInventories.length > 0}
          <div class="flex-1 flex flex-col min-w-0 overflow-hidden">
            <span
              class="fieldset-legend text-base-content/90 mb-1 block text-sm font-medium"
              >Saved Inventories ({savedInventories.length})</span
            >
            <div class="flex gap-3 overflow-x-auto snap-x snap-mandatory pb-1">
              {#each savedInventories as inv}
                <div class="flex-shrink-0 snap-center w-52">
                  <InventoryCard
                    {inv}
                    onLoad={loadInventory}
                    onRename={() => openModal("rename", inv)}
                    onDelete={handleDelete}
                  />
                </div>
              {/each}
            </div>
          </div>
        {/if}
      </div>
      <div class="my-4">
        <span class="label text-base-content/90 mb-1 block">Object types</span>
        <div class="flex flex-wrap items-center gap-2">
          <MultiSelect
            items={supportedTypes}
            bind:selected={selectedTypes}
            placeholder="Types…"
            checkboxColor="checkbox-success"
            disabled={typesDisabled}
            size="w-72"
          />
          <button
            type="button"
            class="btn btn-ghost btn-sm"
            disabled={typesDisabled}
            onclick={() => (selectedTypes = supportedTypes)}>Select all</button
          >
          <button
            type="button"
            class="btn btn-ghost btn-sm"
            disabled={typesDisabled || !selectedTypes.length}
            onclick={() => (selectedTypes = [])}>Clear types</button
          >
        </div>
        <p class="mt-2 text-xs text-base-content/60">
          gfx/, gui/, and music/ object types are not supported. They will not
          appear in reports and may be mistyped if present in processed files.
        </p>
      </div>
      <div class="flex flex-wrap gap-2">
        <button
          type="button"
          class="btn {loading ? 'btn-error' : 'btn-primary'}"
          disabled={loading ? false : extractDisabled}
          onclick={loading
            ? () => {
                extractionPromise?.cancel?.();
                extractionPromise = null;
              }
            : doExtract}
        >
          {loading ? "Cancel" : "Extract"}
        </button>
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
          onclick={clearAll}>Clear results</button
        >
      </div>
    </CardBody>
  </Card>

  <div class="min-w-0">
    {#if extractionErrors.length > 0}
      <div class="alert alert-warning mb-4">
        <span class="font-medium">Errors ({extractionErrors.length}):</span>
        <ul class="list-disc pl-5 text-sm max-h-24 overflow-auto">
          {#each extractionErrors as err}<li>{err}</li>{/each}
        </ul>
      </div>
    {/if}

    {#if hasExtraction}
      <Card class="border border-base-300 shadow-sm">
        <CardBody class="p-4">
          <div class="mb-3 flex items-center justify-between">
            <span class="text-sm text-base-content/70"
              >{allItems.length} items</span
            >
            {#if isCurrentTemp}
              <button
                type="button"
                class="btn btn-primary btn-sm"
                onclick={() => openModal("save")}>Save</button
              >
            {/if}
          </div>
          <Grid
            {columnDefs}
            rowData={allItems}
            gridOptions={{
              pagination: true,
              paginationPageSize: 20,
              onRowClicked: (e) => {
                if (e?.data) {
                  selectedRow = e.data;
                  itemDetailsOpen = true;
                }
              },
            }}
            className="h-[min(28rem,60vh)] w-full rounded border border-base-200"
          />
        </CardBody>
      </Card>
    {:else}
      <div
        class="flex items-center justify-center py-16 text-center text-base-content/60"
      >
        <p>No inventory. Select paths and types, then Extract.</p>
      </div>
    {/if}
  </div>
</div>

<Drawer
  bind:open={itemDetailsOpen}
  side="right"
  defaultSize={580}
  contentClass="max-w-[90vw]"
>
  {#snippet titleSnippet()}Item Details{/snippet}
  {#snippet closeSnippet()}
    <button
      type="button"
      class="btn btn-sm btn-ghost"
      onclick={() => (itemDetailsOpen = false)}>Close</button
    >
  {/snippet}
  <ItemDetails
    inventoryId={currentInventoryId}
    itemType={selectedRow?.type ?? null}
    itemKey={selectedRow?.key ?? null}
    row={selectedRow}
    game={$game}
  />
</Drawer>

<InventoryNameModal
  bind:open={nameModal.open}
  mode={nameModal.mode}
  initialName={nameModalInitialName}
  onsave={handleNameModalSave}
/>

{#if showExportImport}
  <ExportImportDialog
    bind:open={showExportImport}
    {hasExtraction}
    {currentInventoryId}
    game={$game}
    itemCount={allItems.length}
    onImportSuccess={async (id) => {
      const list = await refresh();
      const inv =
        list.find((i) => i.id === id) ??
        ({
          id,
          name: "Imported",
          game: $game,
          createdAt: "",
          totalCount: 0,
        } as InventorySummary);
      await loadInventory(inv);
    }}
  />
{/if}
