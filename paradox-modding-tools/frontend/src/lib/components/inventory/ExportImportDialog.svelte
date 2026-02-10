<script lang="ts">
  import { Dialog, Tabs, Tab } from "@components";

  let {
    open = $bindable(false),
    hasExtraction = false,
  }: { open?: boolean; hasExtraction?: boolean } = $props();

  let exportIncludeRaw = $state(true);
  let exportFormat = $state<"json" | "csv">("json");
  let importData = $state<unknown>(null);
  let importPreview = $state<Record<string, number>>({});

  function onFileSelect(e: Event) {
    const file = (e.target as HTMLInputElement).files?.[0];
    if (!file) return;
    importData = null;
    importPreview = {};
    const r = new FileReader();
    r.onload = (ev) => {
      try {
        const data = JSON.parse(ev.target?.result as string);
        importData = data;
        const prev: Record<string, number> = {};
        if (data && typeof data === "object") {
          for (const [type, result] of Object.entries(data)) {
            const items = (result as { items?: unknown[] })?.items;
            prev[String(type)] = Array.isArray(items) ? items.length : 0;
          }
        }
        importPreview = prev;
      } catch {
        importData = null;
      }
    };
    r.readAsText(file);
  }
</script>

<Dialog bind:open contentProps={{ class: "max-w-[90vw] w-[26rem]" }}>
  {#snippet title()}Export / Import{/snippet}
  {#snippet description()}Inventory{/snippet}
  {#snippet closeDialog()}<button
      type="button"
      class="btn btn-sm btn-ghost"
      onclick={() => (open = false)}>Close</button
    >{/snippet}

  <Tabs class="tabs-border tabs-lg">
    <Tab
      tabGroup="exp-imp"
      label="Export"
      selected={hasExtraction}
      contentClass="p-4"
    >
      <label class="flex items-center gap-2 cursor-pointer mb-4">
        <input
          type="checkbox"
          class="checkbox checkbox-sm"
          bind:checked={exportIncludeRaw}
        />
        <span class="text-sm">Include raw text</span>
      </label>
      <div class="flex gap-4 mb-4">
        <label class="flex items-center gap-2 cursor-pointer"
          ><input
            type="radio"
            name="fmt"
            class="radio radio-sm"
            value="json"
            bind:group={exportFormat}
          /><span class="text-sm">JSON</span></label
        >
        <label class="flex items-center gap-2 cursor-pointer"
          ><input
            type="radio"
            name="fmt"
            class="radio radio-sm"
            value="csv"
            bind:group={exportFormat}
          /><span class="text-sm">CSV</span></label
        >
      </div>
      <p class="text-sm text-base-content/60 mb-4 p-2 bg-base-200 rounded">
        {hasExtraction
          ? "Export from current extraction."
          : "Run extraction first."}
      </p>
      <button
        type="button"
        class="btn btn-primary w-full"
        disabled={!hasExtraction}
        onclick={() => alert("Export not wired yet.")}>Export</button
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
        accept=".json"
        onchange={onFileSelect}
        class="hidden"
        id="imp-file"
      />
      <label for="imp-file" class="btn btn-ghost w-full mb-4 cursor-pointer"
        >Select JSON</label
      >
      {#if Object.keys(importPreview).length > 0}
        <div class="mb-4 p-2 bg-base-200 rounded text-sm">
          {#each Object.entries(importPreview) as [type, count]}<p>
              {type}: {count}
            </p>{/each}
        </div>
      {/if}
      <button
        type="button"
        class="btn btn-primary w-full"
        disabled={!importData}
        onclick={() => {
          alert("Import not wired yet.");
          open = false;
        }}>Import</button
      >
    </Tab>
  </Tabs>
</Dialog>
