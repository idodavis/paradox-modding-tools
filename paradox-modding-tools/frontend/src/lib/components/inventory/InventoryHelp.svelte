<script lang="ts">
  import { Dialog } from "@components";
  let { open = $bindable(false) } = $props();
</script>

<Dialog bind:open size="2xl">
  {#snippet title()}
    <h3 class="text-lg font-bold">Inventory Explorer Help</h3>
  {/snippet}
  {#snippet description()}
    <div class="py-4 text-sm space-y-4">
      <section>
        <h4 class="font-semibold text-base-content mb-2">Extracting Data</h4>
        <p class="text-base-content/90 mb-2">
          Select files or folders to scan, then choose the object types you want
          to extract (e.g., characters, titles, events).
        </p>
        <ul class="list-disc list-inside space-y-1 text-base-content/80">
          <li>Use the file selector to add paths.</li>
          <li>Select types from the dropdown or "Select all".</li>
          <li>
            Click "Extract" to process. Large extractions may take time and
            system resources.
          </li>
        </ul>
      </section>
      <section>
        <h4 class="font-semibold text-base-content mb-2">Exploring Items</h4>
        <p class="text-base-content/90 mb-2">
          The table shows all extracted items. Click a row to view details in
          the side drawer.
        </p>
        <ul class="list-disc list-inside space-y-1 text-base-content/80">
          <li>Filter columns using the inputs in headers.</li>
          <li>Sort by clicking headers.</li>
          <li>
            View references (what objects the selected item mentions) and
            referrers (what objects reference the selected item) in the details
            drawer.
          </li>
        </ul>
      </section>
      <section>
        <h4 class="font-semibold text-base-content mb-2">Saving & Exporting</h4>
        <p class="text-base-content/90 mb-2">
          Extracted inventories are temporary by default. Click "Save" above the
          grid to persist the inventory for later. Use "Export / Import" to save
          data to CSV or load external CSVs. Please note that Super Large
          inventories may not export or import correctly due to size
          limitations. Please deselect some types or unselect raw text to reduce
          size if you encounter issues.
        </p>
      </section>
    </div>
  {/snippet}
</Dialog>
