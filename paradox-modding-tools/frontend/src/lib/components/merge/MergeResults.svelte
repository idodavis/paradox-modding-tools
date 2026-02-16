<script lang="ts">
  import { Card, CardBody, Grid } from "@components";
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
  let validationErrors = $state<
    { path: string; line: number; error: string }[]
  >([]);
  let errorMsg = $state("");

  function openDiff(pathA: string, pathB: string) {
    store.selectedForDiff = { pathA, pathB };
  }

  const summary = $derived.by(() => ({
    files: results.length,
    added: results.reduce(
      (s: number, x: FileMergeResult) => s + (x.added ?? 0),
      0,
    ),
    changed: results.reduce(
      (s: number, x: FileMergeResult) => s + (x.changed ?? 0),
      0,
    ),
    removed: results.reduce(
      (s: number, x: FileMergeResult) => s + (x.removed ?? 0),
      0,
    ),
  }));

  const conflicts = $derived.by(() =>
    results.filter(
      (r: FileMergeResult) =>
        r.resolvedConflicts && r.resolvedConflicts.length > 0,
    ),
  );

  const columns = $derived([
    {
      field: "fileAPath",
      headerName: "A path",
      flex: 2,
      valueFormatter: (p: any) => truncate(p.value),
    },
    {
      field: "fileBPath",
      headerName: "B path",
      flex: 2,
      valueFormatter: (p: any) => truncate(p.value),
    },
    {
      field: "outputPath",
      headerName: "Output",
      flex: 2,
      valueFormatter: (p: any) => truncate(p.value),
    },
    { field: "changed", headerName: "Δ", flex: 1 },
    { field: "added", headerName: "+", flex: 1 },
    { field: "removed", headerName: "-", flex: 1 },
    {
      headerName: "Diff",
      minWidth: 140,
      flex: 3,
      cellRenderer: (params: any) => {
        const d = params.data;
        const div = document.createElement("div");
        div.className = "flex gap-1";
        const btnA = document.createElement("button");
        btnA.className = "btn btn-xs btn-primary";
        btnA.textContent = `${labelA}↔Merged`;
        btnA.onclick = () => openDiff(d.fileAPath, d.outputPath);
        const btnB = document.createElement("button");
        btnB.className = "btn btn-xs btn-secondary";
        btnB.textContent = `${labelB}↔Merged`;
        btnB.onclick = () => openDiff(d.fileBPath, d.outputPath);
        div.append(btnA, btnB);
        return div;
      },
    },
  ]);

  function truncate(p: string) {
    if (!p) return "";
    const parts = p.split(/[/\\]/);
    return parts.length > 2
      ? `.../${parts.slice(-2).join("/")}`
      : (parts.pop() ?? p);
  }

  async function saveReport() {
    if (!results.length) return;
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
      const path = await SaveFile(
        "Save merge report",
        "merge_report.md",
        md,
        "md",
      );
      if (path) showToast({ message: "Report saved", type: "alert-success" });
    } catch (e) {
      errorMsg = e instanceof Error ? e.message : String(e);
    } finally {
      savingReport = false;
    }
  }

  async function runValidation() {
    const outputs = results
      .map((r: FileMergeResult) => r.outputPath)
      .filter(Boolean);
    if (!outputs.length) return;
    validating = true;
    validationErrors = [];
    try {
      const errs = await Merge.ValidateMergedFiles(outputs);
      validationErrors = errs ?? [];
      showToast({
        message: validationErrors.length
          ? `${validationErrors.length} errors`
          : "All valid",
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
    <CardBody>
      {#if errorMsg}
        <div class="text-error text-sm mb-2">{errorMsg}</div>
      {/if}

      <details
        class="mb-4 rounded-lg border border-base-content/20 bg-base-200/50 overflow-hidden group"
        open={conflicts.length > 0}
      >
        <summary
          class="px-4 py-3 flex flex-wrap justify-between items-center gap-2 cursor-pointer select-none list-none hover:bg-base-200 transition-colors"
        >
          <div class="flex flex-wrap items-center gap-2">
            <span class="font-semibold">Summary</span>
            <span class="badge badge-primary badge-sm"
              >{summary.files} files</span
            >
            <span class="badge badge-success badge-sm"
              >+{summary.added} added</span
            >
            <span class="badge badge-warning badge-sm"
              >{summary.changed} changed</span
            >
            <span class="badge badge-error badge-sm"
              >-{summary.removed} removed</span
            >
            {#if conflicts.length > 0}
              <span class="badge badge-warning badge-sm ml-2">
                {conflicts.length} resolved conflicts (click to view)
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
          <div
            class="px-4 py-3 space-y-3 text-sm border-t border-base-content/10 bg-base-100"
          >
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
                        >{c.usedSide === "A"
                          ? labelA
                          : c.usedSide === "B"
                            ? labelB
                            : c.usedSide}</span
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
        <details class="mb-4 rounded-lg border border-error/30 bg-error/5" open>
          <summary
            class="px-3 py-2 text-sm font-medium cursor-pointer text-error bg-base-200/80"
            >Validation errors ({validationErrors.length})</summary
          >
          <ul
            class="px-3 py-3 space-y-2 text-sm text-error/90 border-t border-base-content/10"
          >
            {#each validationErrors as ve}
              <li
                class="flex gap-2 flex-wrap border border-error/20 bg-base-100 px-2 py-1 rounded"
              >
                <code>{ve.path}</code> <span>L{ve.line}: {ve.error}</span>
              </li>
            {/each}
          </ul>
        </details>
      {/if}
      <Grid
        columnDefs={columns}
        rowData={results}
        className="min-h-[560px] h-[min(70vh,600px)] w-full"
      />
    </CardBody>
  </Card>
</section>
