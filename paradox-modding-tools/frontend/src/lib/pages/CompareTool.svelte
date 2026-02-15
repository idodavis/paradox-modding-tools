<script lang="ts">
  import {
    Tabs,
    Tab,
    Card,
    CardBody,
    FileSelector,
    Grid,
    DiffViewer,
  } from "@components";
  import { game, gameInstallPath } from "@stores/app.svelte";
  import { VanillaCompare, TwoSetsCompare } from "@services/compareservice";
  import type { PathMatch } from "@services/models";
  let modPaths: string[] = $state([]);
  let setAPaths: string[] = $state([]);
  let setBPaths: string[] = $state([]);
  let fileAPath: string = $state("");
  let fileBPath: string = $state("");
  let matchingFiles = $state<Record<string, PathMatch | undefined>>({});
  let selectedForDiff = $state<MatchRow | null>(null);

  $effect(() => {
    $game;
    matchingFiles = {};
    selectedForDiff = null;
  });

  async function runVanillaCompare() {
    matchingFiles = await VanillaCompare($game, $gameInstallPath, modPaths);
  }
  async function runTwoSetsCompare() {
    matchingFiles = await TwoSetsCompare(setAPaths, setBPaths);
  }
  async function runAnyTwoFilesCompare() {
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
  const columns = $derived([
    { field: "relativePath", headerName: "Relative path", flex: 10 },
    {
      headerName: "Show Diff",
      cellRenderer: (params: { data: MatchRow }) => {
        const btn = document.createElement("button");
        btn.className = "btn btn-sm btn-primary btn-outline";
        btn.textContent = "Show Diff";
        btn.onclick = () => {
          selectedForDiff = params.data;
        };
        return btn;
      },
      flex: 1,
    },
  ]);
</script>

<div class="p-4">
  <Tabs class="tabs-border tabs-xl">
    <Tab
      tabGroup="compare-mode"
      label="Vanilla vs Mod"
      selected
      contentClass="bg-base-300 border-base-300 p-6 "
    >
      <Card>
        <CardBody>
          <p class="text-base text-base-content/90 mb-4">
            Using Vanilla (A) as the base, select your mod (B) to compare with:
          </p>
          <div class="mb-4">
            <fieldset class="fieldset mb-4">
              <legend class="fieldset-legend text-base-content/90"
                >Vanilla (A):</legend
              >
              <input
                type="text"
                class="input w-full max-w-2xl"
                readonly
                value={$gameInstallPath}
                placeholder="Set game install path in Modding Docs or header settings"
              />
              <p class="label">
                Based on current game: {$game} - (root script directory:
                {#if $game === "CK3"}
                  game
                {:else}
                  game/in_game
                {/if})
              </p>
            </fieldset>
            <FileSelector
              mode="folder"
              dialogTitle="Select Mod (B) files/folders"
              btnText="Browse"
              placeholder="Select folder or files to compare with Vanilla"
              initialValue={modPaths}
              onPathsChange={(paths: string[]) => (modPaths = paths)}
            />
          </div>
          <button
            type="button"
            class="btn btn-soft btn-wide btn-primary"
            onclick={runVanillaCompare}
            disabled={$gameInstallPath === ""}
          >
            Run Compare
          </button>
        </CardBody>
      </Card>
    </Tab>
    <Tab
      tabGroup="compare-mode"
      label="Two Sets / Directories"
      contentClass="bg-base-300 border-base-300 p-6"
    >
      <Card>
        <CardBody>
          <p class="text-base text-base-content/90 mb-4">
            Select two sets of files/directories to compare:
          </p>
          <div class="mb-4">
            <FileSelector
              mode="folder"
              dialogTitle="Select Set A files/folders"
              btnText="Browse"
              placeholder="Select folder or files for Set A"
              initialValue={setAPaths}
              onPathsChange={(paths: string[]) => (setAPaths = paths)}
            />
            <FileSelector
              mode="folder"
              dialogTitle="Select Set B files/folders"
              btnText="Browse"
              placeholder="Select folder or files for Set B"
              initialValue={setBPaths}
              onPathsChange={(paths: string[]) => (setBPaths = paths)}
            />
          </div>
          <button
            type="button"
            class="btn btn-soft btn-wide btn-primary"
            onclick={runTwoSetsCompare}
            disabled={setAPaths.length === 0 || setBPaths.length === 0}
          >
            Run Compare
          </button>
        </CardBody>
      </Card>
    </Tab>
    <Tab
      tabGroup="compare-mode"
      label="Any Two Files"
      contentClass="bg-base-300 border-base-300 p-6"
    >
      <Card>
        <CardBody>
          <p class="text-base text-base-content/90 mb-4">
            Select two files to compare:
          </p>
          <div class="mb-4">
            <FileSelector
              mode="file"
              dialogTitle="Select File A"
              btnText="Select File"
              placeholder="Select file for File A"
              initialValue={[fileAPath]}
              onPathsChange={(paths: string[]) => (fileAPath = paths[0])}
            />
            <FileSelector
              mode="file"
              dialogTitle="Select File B"
              btnText="Select File"
              placeholder="Select file for File B"
              initialValue={[fileBPath]}
              onPathsChange={(paths: string[]) => (fileBPath = paths[0])}
            />
          </div>
          <button
            type="button"
            class="btn btn-soft btn-wide btn-primary"
            onclick={runAnyTwoFilesCompare}
            disabled={fileAPath === "" || fileBPath === ""}
          >
            Run Compare
          </button>
        </CardBody>
      </Card>
    </Tab>
  </Tabs>
  <Card class="px-10">
    <CardBody>
      <h3 class="card-title justify-center mb-4">Results</h3>
      <Grid
        columnDefs={columns}
        rowData={rows}
        className="h-88 border-base-200 border-4 rounded-lg"
      />
    </CardBody>
  </Card>
</div>

{#if selectedForDiff}
  <DiffViewer
    oldFile={selectedForDiff.pathA}
    newFile={selectedForDiff.pathB}
    onclose={() => (selectedForDiff = null)}
  />
{/if}
