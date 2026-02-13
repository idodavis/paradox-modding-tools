<script lang="ts">
  import { Dialog, Tabs, Tab } from "@components";
  import * as InventoryService from "@services/inventoryservice";
  import { SaveFile } from "@services/fileservice";

  const LARGE_INVENTORY_THRESHOLD = 5000;

  let {
    open = $bindable(false),
    hasExtraction = false,
    currentInventoryId = null,
    game = "",
    itemCount = 0,
    onImportSuccess,
  }: {
    open?: boolean;
    hasExtraction?: boolean;
    currentInventoryId?: string | null;
    game?: string;
    itemCount?: number;
    onImportSuccess?: (inventoryId: string) => void;
  } = $props();

  const isLargeInventory = $derived(itemCount >= LARGE_INVENTORY_THRESHOLD);

  let exportIncludeRaw = $state(true);
  let importData = $state<string | null>(null);
  let importPreview = $state<number>(0);
  let exportBusy = $state(false);
  let importBusy = $state(false);
  let error = $state<string | null>(null);

  function onFileSelect(e: Event) {
    const file = (e.target as HTMLInputElement).files?.[0];
    if (!file) return;
    importData = null;
    importPreview = 0;
    error = null;
    const r = new FileReader();
    r.onload = (ev) => {
      const text = ev.target?.result as string;
      if (!text) return;
      importData = text;
      try {
        const lines = text.trim().split(/\r?\n/);
        importPreview = Math.max(0, lines.length - 1); // minus header
      } catch {
        importPreview = 0;
      }
    };
    r.readAsText(file);
  }

  async function doExport() {
    if (!currentInventoryId) return;
    exportBusy = true;
    error = null;
    try {
      const content = await InventoryService.ExportInventory(
        currentInventoryId,
        exportIncludeRaw,
      );
      await SaveFile("Export inventory", "inventory.csv", content, "csv");
      open = false;
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      exportBusy = false;
    }
  }

  async function doImport() {
    if (!importData || !game) return;
    importBusy = true;
    error = null;
    try {
      const newId = await InventoryService.ImportInventory(game, importData);
      onImportSuccess?.(newId);
      open = false;
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      importBusy = false;
    }
  }
</script>

<Dialog
  bind:open
  contentProps={{
    class: "max-w-[92vw] w-[44rem] max-h-[min(90vh,38rem)] pt-6",
  }}
>
  {#snippet title()}
    <div class="flex flex-col gap-0.5 -my-1">
      <span class="text-base font-semibold tracking-tight">Export & Import</span
      >
      <span class="text-xs font-normal text-base-content/60"
        >Save inventory to CSV or load from CSV</span
      >
    </div>
  {/snippet}
  {#snippet description()}
    <span class="sr-only"
      >Export or import inventory data to or from a CSV file.</span
    >
  {/snippet}
  {#snippet closeDialog()}
    <button
      type="button"
      class="btn btn-sm btn-ghost"
      onclick={() => (open = false)}>Close</button
    >
  {/snippet}

  <div class="mt-3 pt-3 border-t border-base-300">
    <Tabs class="tabs-border tabs-lg">
      <Tab
        tabGroup="exp-imp"
        label="Export"
        selected={hasExtraction}
        contentClass="p-4"
      >
        {#if isLargeInventory}
          <div class="alert alert-warning mb-4 text-sm flex flex-col">
            <span class="font-medium"
              >Large inventory ({itemCount.toLocaleString()} items).</span
            >
            <span class="block mt-1">
              Uncheck "Include raw text" to avoid crashes or out-of-memory — raw
              text makes exports much larger.
            </span>
          </div>
        {/if}
        <label class="flex items-center gap-2 cursor-pointer mb-4">
          <input
            type="checkbox"
            class="checkbox checkbox-sm"
            bind:checked={exportIncludeRaw}
          />
          <span class="text-sm">Include raw text</span>
        </label>
        <p class="text-sm text-base-content/60 mb-4 p-2 bg-base-200 rounded">
          {hasExtraction
            ? "Export current inventory to a CSV file."
            : "Run extraction or load an inventory first."}
        </p>
        <button
          type="button"
          class="btn btn-primary w-full"
          disabled={!hasExtraction || exportBusy}
          onclick={doExport}>{exportBusy ? "Exporting…" : "Export"}</button
        >
      </Tab>
      <Tab
        tabGroup="exp-imp"
        label="Import"
        selected={!hasExtraction}
        contentClass="p-4"
      >
        <input
          type="file"
          accept=".csv"
          onchange={onFileSelect}
          class="hidden"
          id="imp-file"
        />
        <label for="imp-file" class="btn btn-ghost w-full mb-4 cursor-pointer"
          >Select CSV file</label
        >
        {#if importPreview > 0}
          <div class="mb-4 p-2 bg-base-200 rounded text-sm">
            {importPreview} items
          </div>
        {/if}
        <button
          type="button"
          class="btn btn-primary w-full"
          disabled={!importData || importBusy}
          onclick={doImport}>{importBusy ? "Importing…" : "Import"}</button
        >
      </Tab>
    </Tabs>
    {#if error}
      <div class="mt-3 p-3 rounded-lg bg-error/10 text-error text-sm">
        {error}
      </div>
    {/if}
  </div>
</Dialog>
