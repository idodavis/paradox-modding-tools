<script lang="ts">
  import Icon from "@iconify/svelte";
  import { Tabs, Tab, Card, CardBody } from "../components";
  import { game, gameInstallPathCk3, gameInstallPathEu5 } from "../stores/app";
  import {
    SelectFiles,
    SelectDirectories,
  } from "../../../bindings/paradox-modding-tools/services/fileservice";

  let selectedPaths = $state<string[]>([]);

  async function selectFiles() {
    selectedPaths = selectedPaths.concat(
      await SelectFiles("Select files", "*.txt"),
    );
  }

  async function selectDirectories() {
    selectedPaths = selectedPaths.concat(
      await SelectDirectories("Select folder(s)"),
    );
  }

  function clear() {
    selectedPaths = [];
  }
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
            <fieldset class="fieldset">
              <legend class="fieldset-legend text-base-content/90"
                >Mod (B):</legend
              >
              <textarea
                class="textarea w-full max-w-2xl"
                readonly
                value={selectedPaths.join("\n")}
                placeholder="Select folder or files to compare with Vanilla"
              ></textarea>
              <div class="flex flex-wrap gap-2 max-w-2xl w-full">
                <button
                  type="button"
                  class="btn btn-soft btn-secondary w-39"
                  onclick={selectFiles}
                >
                  Browse Files
                </button>
                <button
                  type="button"
                  class="btn btn-soft btn-secondary w-39"
                  onclick={selectDirectories}
                >
                  Browse Folders
                </button>
                <button type="button" class="btn w-25 ml-auto" onclick={clear}>
                  Clear
                  <Icon icon="mdi:trash-can" class="size-4" />
                </button>
              </div>
            </fieldset>
          </div>
        </CardBody>
      </Card>
      <Card class="mt-10 border border-base-content/40">
        <CardBody>
          <h3 class="card-title justify-center mb-10">Matching Files</h3>
          <p class="text-center">
            Select Mod (B) files/folders, then click Compare.
          </p>
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
