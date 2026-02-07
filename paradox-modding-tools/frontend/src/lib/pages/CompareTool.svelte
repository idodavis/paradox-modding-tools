<script lang="ts">
  import { Tabs, Tab, Card, CardBody, FileSelector, Grid } from "@components";
  import { game, gameInstallPathCk3, gameInstallPathEu5 } from "@stores/app";
  import {
    GetGameScriptRoot,
    CollectFilesFromPaths,
    FindMatchingFiles,
  } from "@services/fileservice";
  import type { FileMatch } from "@services/models";
  import { showToast } from "@stores/toast";

  const gameInstallPath = $derived(
    $game === "CK3" ? $gameInstallPathCk3 : $gameInstallPathEu5,
  );
  let modPaths = $state<string[]>([]);
  let matchingFiles = $state<{ [key: string]: FileMatch }>({});

  async function runCompare() {
    const gameScriptRoot = await GetGameScriptRoot($game, gameInstallPath);
    const filesA = await CollectFilesFromPaths([gameScriptRoot]);
    const filesB = await CollectFilesFromPaths(modPaths);
    matchingFiles = await FindMatchingFiles(filesA, filesB);
  }
  type MatchRow = FileMatch & { relativePath: string };
  const rows = $derived(
    Object.entries(matchingFiles).map(([relativePath, match]) => ({
      relativePath,
      ...match,
    })) as MatchRow[],
  );
  const columns = $derived([
    { field: "relativePath", headerName: "Relative path" },
    {
      headerName: "Show Diff",
      cellRenderer: (params: { data: MatchRow }) => {
        const btn = document.createElement("button");
        btn.className = "btn btn-sm btn-primary";
        btn.textContent = "Show Diff";
        btn.onclick = () => {
          if (params.data) console.log("Show diff:", params.data);
        };
        return btn;
      },
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
                value={$game === "CK3"
                  ? $gameInstallPathCk3
                  : $gameInstallPathEu5}
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
              mode="filesAndFolders"
              dialogTitle="Select Mod (B) files/folders"
              fileBtnText="Select Files"
              folderBtnText="Select Folders"
              placeholder="Select folder or files to compare with Vanilla"
              initialValue={modPaths}
              onPathsChange={(paths: string[]) => (modPaths = paths)}
            />
          </div>
          <button
            type="button"
            class="btn btn-soft btn-wide btn-primary"
            onclick={runCompare}
            disabled={gameInstallPath === ""}
          >
            Run Compare
          </button>
        </CardBody>
      </Card>
      <Card class="mt-10 border border-base-content/40">
        <CardBody>
          <h3 class="card-title justify-center mb-10">Matching Files</h3>
          <Grid columnDefs={columns} rowData={rows} />
        </CardBody>
      </Card>
    </Tab>
    <Tab
      tabGroup="compare-mode"
      label="Two Sets / Directories"
      contentClass="bg-base-300 border-base-300 p-6">Two sets / directories</Tab
    >
    <Tab
      tabGroup="compare-mode"
      label="Any Two Files"
      contentClass="bg-base-300 border-base-300 p-6">Any two files</Tab
    >
  </Tabs>
</div>
