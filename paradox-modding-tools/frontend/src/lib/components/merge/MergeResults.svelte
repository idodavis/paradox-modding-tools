<script lang="ts">
  import { Card, CardBody, Grid, SplitPane, DiffPaneContent, Dialog } from "@components";
  import { showToast } from "@stores/toast.svelte";
  import * as MergeService from "@services/mergeservice";
  import { SaveFile } from "@services/fileservice";
  import type { FileMergeResult } from "@services/models";
  import { getMergeStore } from "@stores/merge.svelte";

  const store = getMergeStore();
  const results = $derived(store.mergeResults);
  const { a: labelA, b: labelB } = $derived(store.labels);

  const Merge = MergeService as any;

  let savingReport = $state(false);
  let validating = $state(false);
  let validationErrors = $state<{ path: string; line: number; error: string }[]>([]);
  let errorMsg = $state("");

  // Split-pane diff state
  let selectedIndex = $state<number | null>(null);
  let diffSide = $state<"A" | "B">("A");
  let showFullscreen = $state(false);
  let gridApi = $state<any>(null);

  function navigateTo(i: number) {
    selectedIndex = i;
    gridApi?.getDisplayedRowAtIndex(i)?.setSelected(true, true);
    gridApi?.ensureIndexVisible(i, "middle");
  }

  const selectedResult = $derived(selectedIndex !== null ? (results[selectedIndex] ?? null) : null);

  const currentOldFile = $derived(
    selectedResult ? (diffSide === "A" ? selectedResult.fileAPath : selectedResult.fileBPath) : "",
  );
  const currentNewFile = $derived(selectedResult?.outputPath ?? "");
  const currentOldFileName = $derived(selectedResult ? (diffSide === "A" ? labelA : labelB) : "");
  const currentOldColor = $derived(diffSide === "A" ? "text-primary" : "text-secondary");

  // Reset selectedIndex when results change
  $effect(() => {
    results;
    selectedIndex = null;
  });

  const summary = $derived.by(() => ({
    files: results.length,
    added: results.reduce((s: number, x: FileMergeResult) => s + (x.added ?? 0), 0),
    changed: results.reduce((s: number, x: FileMergeResult) => s + (x.changed ?? 0), 0),
    removed: results.reduce((s: number, x: FileMergeResult) => s + (x.removed ?? 0), 0),
  }));

  const conflicts = $derived(results.filter((r: FileMergeResult) => (r.resolvedConflicts?.length ?? 0) > 0));

  function truncate(p: string) {
    const parts = p.split(/[/\\]/);
    return parts.length > 2 ? `.../ ${parts.slice(-2).join("/")}` : (parts.pop() ?? p);
  }

  const columns = [
    {
      field: "filePath",
      headerName: "File",
      flex: 3,
      valueFormatter: (p: any) => truncate(p.value),
    },
    { field: "changed", headerName: "Δ", flex: 1, maxWidth: 70 },
    { field: "added", headerName: "+", flex: 1, maxWidth: 70 },
    { field: "removed", headerName: "-", flex: 1, maxWidth: 70 },
  ];

  async function saveReport() {
    savingReport = true;
    try {
      const md = await Merge.GenerateMergeReport(
        results,
        summary.added,
        summary.changed,
        summary.removed,
        labelA,
        labelB,
      );
      const path = await SaveFile("Save merge report", "merge_report.md", md, "md");
      if (path) showToast({ message: "Report saved", type: "alert-success" });
    } catch (e) {
      errorMsg = e instanceof Error ? e.message : String(e);
    } finally {
      savingReport = false;
    }
  }

  async function runValidation() {
    const outputs = results.map((r: FileMergeResult) => r.outputPath).filter(Boolean);
    validating = true;
    validationErrors = [];
    try {
      const errs = await Merge.ValidateMergedFiles(outputs);
      validationErrors = errs ?? [];
      showToast({
        message: validationErrors.length ? `${validationErrors.length} errors` : "All valid",
        type: validationErrors.length ? "alert-warning" : "alert-success",
      });
    } catch (e) {
      errorMsg = e instanceof Error ? e.message : String(e);
    } finally {
      validating = false;
    }
  }
</script>

<section class="mt-6">
  <h3 class="text-sm font-semibold text-base-content/90 mb-3">Results</h3>
  <Card>
    <CardBody class="!p-0">
      {#if errorMsg}
        <div class="text-error text-sm p-3">{errorMsg}</div>
      {/if}

      <details class="border-b border-base-content/20 bg-base-200/50 overflow-hidden" open={conflicts.length > 0}>
        <summary
          class="px-4 py-3 flex flex-wrap justify-between items-center gap-2 cursor-pointer select-none list-none hover:bg-base-200 transition-colors"
        >
          <div class="flex flex-wrap items-center gap-2">
            <span class="font-semibold">Summary</span>
            <span class="badge badge-primary badge-sm">{summary.files} files</span>
            <span class="badge badge-success badge-sm">+{summary.added} added</span>
            <span class="badge badge-warning badge-sm">{summary.changed} changed</span>
            <span class="badge badge-error badge-sm">-{summary.removed} removed</span>
            {#if conflicts.length > 0}
              <span class="badge badge-warning badge-sm ml-2">
                {conflicts.length} resolved conflicts
              </span>
            {/if}
          </div>
          <div class="flex gap-2">
            <button
              type="button"
              class="btn btn-sm btn-ghost"
              disabled={savingReport}
              onclick={(e) => {
                e.stopPropagation();
                saveReport();
              }}>{savingReport ? "Saving…" : "Save report"}</button
            >
            <button
              type="button"
              class="btn btn-sm btn-ghost"
              disabled={validating}
              onclick={(e) => {
                e.stopPropagation();
                runValidation();
              }}>{validating ? "Validating…" : "Validate"}</button
            >
          </div>
        </summary>

        {#if conflicts.length > 0}
          <div class="px-4 py-3 space-y-3 text-sm border-t border-base-content/10 bg-base-100">
            {#each conflicts as file}
              <div class="rounded border border-warning/20 bg-warning/5 p-2">
                <div class="font-medium mb-1 text-base-content/80">
                  {file.filePath}
                </div>
                <ul class="space-y-1 ml-2">
                  {#each (file as any).resolvedConflicts as c}
                    <li class="flex items-center gap-2 text-xs">
                      <code class="bg-base-200 px-1 rounded">{c.key}</code>
                      <span class="text-base-content/60">→</span>
                      <span
                        class="font-medium"
                        class:text-primary={c.usedSide === "A"}
                        class:text-secondary={c.usedSide === "B"}
                        >{c.usedSide === "A" ? labelA : c.usedSide === "B" ? labelB : c.usedSide}</span
                      >
                      <span class="text-base-content/50">({c.reason})</span>
                    </li>
                  {/each}
                </ul>
              </div>
            {/each}
          </div>
        {/if}
      </details>

      {#if validationErrors.length > 0}
        <details class="border-b border-error/30 bg-error/5" open>
          <summary class="px-3 py-2 text-sm font-medium cursor-pointer text-error bg-base-200/80">
            Validation errors ({validationErrors.length})
          </summary>
          <ul class="p-3 space-y-2 text-sm text-error/90 border-t border-base-content/10">
            {#each validationErrors as ve}
              <li class="flex gap-2 flex-wrap border border-error/20 bg-base-100 px-2 py-1 rounded">
                <code>{ve.path}</code> <span>L{ve.line}: {ve.error}</span>
              </li>
            {/each}
          </ul>
        </details>
      {/if}

      <SplitPane secondOpen={selectedIndex !== null} defaultSecondSize={580} class="h-svh">
        {#snippet first()}
          <Grid
            columnDefs={columns}
            rowData={results}
            className="h-full w-full"
            gridOptions={{
              rowSelection: "single",
              onGridReady: (e: any) => {
                gridApi = e.api;
              },
              onRowClicked: (e: any) => {
                selectedIndex = e.rowIndex;
              },
              getRowStyle: (params: any) =>
                params.rowIndex === selectedIndex ? { background: "oklch(var(--p, 0.5 0.2 250)/0.15)" } : undefined,
            }}
          />
        {/snippet}

        {#snippet second()}
          <DiffPaneContent
            oldFile={currentOldFile}
            newFile={currentNewFile}
            oldFileName={currentOldFileName}
            newFileName="Merged Output"
            oldFileColor={currentOldColor}
            newFileColor="text-accent"
            hasPrev={selectedIndex !== null && selectedIndex > 0}
            hasNext={selectedIndex !== null && selectedIndex < results.length - 1}
            navLabel={selectedIndex !== null ? `${selectedIndex + 1} / ${results.length}` : undefined}
            onPrev={() => {
              if (selectedIndex !== null && selectedIndex > 0) navigateTo(selectedIndex - 1);
            }}
            onNext={() => {
              if (selectedIndex !== null && selectedIndex < results.length - 1) navigateTo(selectedIndex + 1);
            }}
            onFullscreen={() => (showFullscreen = true)}
          >
            {#snippet extraHeader()}
              <div class="flex items-center gap-2">
                <span class="text-xs font-medium text-base-content/60">Compare:</span>
                <div class="join">
                  <button
                    type="button"
                    class="join-item btn btn-sm btn-outline"
                    class:btn-primary={diffSide === "A"}
                    onclick={() => (diffSide = "A")}>{labelA}↔Merged</button
                  >
                  <button
                    type="button"
                    class="join-item btn btn-sm btn-outline"
                    class:btn-secondary={diffSide === "B"}
                    onclick={() => (diffSide = "B")}>{labelB}↔Merged</button
                  >
                </div>
              </div>
            {/snippet}
          </DiffPaneContent>
        {/snippet}
      </SplitPane>
    </CardBody>
  </Card>
</section>

<Dialog
  bind:open={showFullscreen}
  size="fullscreen"
  contentProps={{ class: "flex flex-col overflow-hidden !p-0 bg-base-100" }}
>
  {#snippet title()}
    <div class="px-4 py-2 border-b border-base-content/20 bg-base-200 flex items-center justify-between shrink-0">
      <h2 class="text-base font-semibold">File Comparison View</h2>
      <button type="button" class="btn btn-ghost btn-sm" onclick={() => (showFullscreen = false)}>Close</button>
    </div>
  {/snippet}
  {#snippet description()}<span class="sr-only">Diff viewer</span>{/snippet}
  {#if showFullscreen && selectedResult}
    <DiffPaneContent
      oldFile={currentOldFile}
      newFile={currentNewFile}
      oldFileName={currentOldFileName}
      newFileName="Merged Output"
      oldFileColor={currentOldColor}
      newFileColor="text-accent"
    />
  {/if}
</Dialog>
