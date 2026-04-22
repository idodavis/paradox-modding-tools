<script lang="ts">
  import type { GridApi } from "ag-grid-community";
  import { Tabs, Tab, Card, CardBody, FileSelector, Grid, SplitPane, DiffPaneContent, Dialog } from "@components";
  import { game, gameInstallPath } from "@stores/app.svelte";
  import { VanillaCompare, DirectoryCompare } from "@services/compareservice";
  import type { PathMatch } from "@services/models";
  let modPath: string = $state("");
  let setAPath: string = $state("");
  let setBPath: string = $state("");
  let fileAPath: string = $state("");
  let fileBPath: string = $state("");
  let matchingFiles = $state<Record<string, PathMatch | undefined>>({});
  let selectedIndex = $state<number | null>(null);
  let showFullscreen = $state(false);
  let gridApi = $state<GridApi | null>(null);

  function navigateTo(i: number) {
    selectedIndex = i;
    gridApi?.getDisplayedRowAtIndex(i)?.setSelected(true, true);
    gridApi?.ensureIndexVisible(i, "middle");
  }

  $effect(() => {
    $game;
    matchingFiles = {};
    selectedIndex = null;
  });

  async function runVanillaCompare() {
    matchingFiles = await VanillaCompare($game, $gameInstallPath, modPath);
  }
  async function runDirectoryCompare() {
    matchingFiles = await DirectoryCompare(setAPath, setBPath);
  }
  async function runFileCompare() {
    matchingFiles = {
      "Comparing Two Files": {
        pathA: fileAPath,
        pathB: fileBPath,
      } as PathMatch,
    };
  }

  type MatchRow = PathMatch & { relativePath: string };
  const rows = $derived(
    Object.entries(matchingFiles)
      .filter(([, match]) => match !== undefined)
      .map(([relativePath, match]) => ({
        relativePath,
        ...match,
      })) as MatchRow[],
  );
  const selectedRow = $derived(selectedIndex !== null ? (rows[selectedIndex] ?? null) : null);
  const columns = [
    {
      field: "relativePath",
      headerName: "Relative path",
      flex: 1,
      tooltipValueGetter: (p: { data?: MatchRow; value?: string }) =>
        p.data?.relativePath === "Comparing Two Files"
          ? "Click to view diff. In File vs File mode, the two files may not share a common path."
          : (p.value ?? ""),
    },
  ];
</script>

<div class="relative p-4 max-w-full min-w-0">
  <Tabs class="tabs-border tabs-xl">
    <Tab tabGroup="compare-mode" label="Vanilla vs Mod" selected contentClass="bg-base-200/50 border-base-content/10 p-6">
      <Card>
        <CardBody>
          <p class="text-base text-base-content/90 mb-4">
            Using Vanilla (A) as the base, select your mod (B) to compare with:
          </p>
          <div class="mb-4">
            <fieldset class="fieldset mb-4">
              <legend class="fieldset-legend text-base-content/90">Vanilla (A):</legend>
              <input
                type="text"
                class="input w-full max-w-2xl"
                readonly
                value={$gameInstallPath}
                placeholder="Set game install path in Modding Docs or header settings"
              />
              <p class="label">
                Based on current game: {$game} - (root script directory:
                {#if $game === "CK3"}game{:else}game/in_game{/if})
              </p>
            </fieldset>
            <FileSelector
              mode="folder"
              dialogTitle="Select Mod (B) files/folders"
              btnText="Browse"
              placeholder="Select folder or files to compare with Vanilla"
              initialValue={modPath}
              onPathChange={(p) => (modPath = p ?? "")}
            />
          </div>
          <div class="flex flex-wrap gap-2 pt-4 border-t border-base-content/10">
            <button
              type="button"
              class="btn btn-soft btn-wide btn-primary"
              onclick={runVanillaCompare}
              disabled={$gameInstallPath === "" || modPath === ""}>Run Compare</button
            >
            <button
              type="button"
              class="btn btn-ghost text-error hover:bg-error/10"
              onclick={() => {
                matchingFiles = {};
                selectedIndex = null;
              }}>Clear Results</button
            >
          </div>
        </CardBody>
      </Card>
    </Tab>
    <Tab tabGroup="compare-mode" label="Directory vs Directory" contentClass="bg-base-200/50 border-base-content/10 p-6">
      <Card>
        <CardBody>
          <p class="text-base text-base-content/90 mb-4">Select two sets of files/directories to compare:</p>
          <div class="mb-4">
            <FileSelector
              mode="folder"
              dialogTitle="Select Set A files/folders"
              btnText="Browse"
              placeholder="Select folder or files for Set A"
              initialValue={setAPath}
              onPathChange={(p) => (setAPath = p ?? "")}
            />
            <FileSelector
              mode="folder"
              dialogTitle="Select Set B files/folders"
              btnText="Browse"
              placeholder="Select folder or files for Set B"
              initialValue={setBPath}
              onPathChange={(p) => (setBPath = p ?? "")}
            />
          </div>
          <div class="flex flex-wrap gap-2 pt-4 border-t border-base-content/10">
            <button
              type="button"
              class="btn btn-soft btn-wide btn-primary"
              onclick={runDirectoryCompare}
              disabled={setAPath === "" || setBPath === ""}>Run Compare</button
            >
            <button
              type="button"
              class="btn btn-ghost text-error hover:bg-error/10"
              onclick={() => {
                matchingFiles = {};
                selectedIndex = null;
              }}>Clear Results</button
            >
          </div>
        </CardBody>
      </Card>
    </Tab>
    <Tab tabGroup="compare-mode" label="File vs File" contentClass="bg-base-200/50 border-base-content/10 p-6">
      <Card>
        <CardBody>
          <p class="text-base text-base-content/90 mb-4">Select two files to compare:</p>
          <div class="mb-4">
            <FileSelector
              mode="file"
              dialogTitle="Select File A"
              btnText="Select File"
              placeholder="Select file for File A"
              initialValue={fileAPath}
              onPathChange={(p) => (fileAPath = p ?? "")}
            />
            <FileSelector
              mode="file"
              dialogTitle="Select File B"
              btnText="Select File"
              placeholder="Select file for File B"
              initialValue={fileBPath}
              onPathChange={(p) => (fileBPath = p ?? "")}
            />
          </div>
          <div class="flex flex-wrap gap-2 pt-4 border-t border-base-content/10">
            <button
              type="button"
              class="btn btn-soft btn-wide btn-primary"
              onclick={runFileCompare}
              disabled={fileAPath === "" || fileBPath === ""}>Run Compare</button
            >
            <button
              type="button"
              class="btn btn-ghost text-error hover:bg-error/10"
              onclick={() => {
                matchingFiles = {};
                selectedIndex = null;
              }}>Clear Results</button
            >
          </div>
        </CardBody>
      </Card>
    </Tab>
  </Tabs>
  {#if rows.length > 0}
    <h3 class="mt-6 text-sm font-semibold text-base-content/90 mb-3">Results</h3>
    <Card>
      <CardBody class="p-0!">
        <SplitPane secondOpen={selectedIndex !== null} defaultSecondSize={580} class="h-[calc(93.5vh-6rem)]">
          {#snippet first()}
            <Grid
              columnDefs={columns}
              rowData={rows}
              className="h-full w-full"
              gridOptions={{
                rowSelection: "single",
                enableBrowserTooltips: true,
                tooltipShowDelay: 200,
                onGridReady: (e) => {
                  gridApi = e.api;
                },
                onRowClicked: (e) => {
                  selectedIndex = e.rowIndex;
                },
              }}
            />
          {/snippet}

          {#snippet second()}
            <DiffPaneContent
              oldFile={selectedRow?.pathA ?? ""}
              newFile={selectedRow?.pathB ?? ""}
              hasPrev={selectedIndex !== null && selectedIndex > 0}
              hasNext={selectedIndex !== null && selectedIndex < rows.length - 1}
              navLabel={selectedIndex !== null ? `${selectedIndex + 1} / ${rows.length}` : undefined}
              onPrev={() => {
                if (selectedIndex !== null && selectedIndex > 0) navigateTo(selectedIndex - 1);
              }}
              onNext={() => {
                if (selectedIndex !== null && selectedIndex < rows.length - 1) navigateTo(selectedIndex + 1);
              }}
              onFullscreen={() => (showFullscreen = true)}
            />
          {/snippet}
        </SplitPane>
      </CardBody>
    </Card>
  {/if}
</div>

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
  {#if showFullscreen && selectedRow}
    <DiffPaneContent oldFile={selectedRow.pathA} newFile={selectedRow.pathB} />
  {/if}
</Dialog>
