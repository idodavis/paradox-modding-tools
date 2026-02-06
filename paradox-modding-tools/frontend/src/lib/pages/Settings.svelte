<script lang="ts">
  import { onMount } from "svelte";
  import { Card, CardBody } from "../components";
  import { SelectDirectory } from "../../../bindings/paradox-modding-tools/services/fileservice";
  import {
    goBack,
    gameInstallPathCk3,
    gameInstallPathEu5,
    saveSettings,
    loadSettings,
  } from "../stores/app";

  onMount(() => {
    loadSettings();
  });

  async function save() {
    await saveSettings();
  }

  async function browseGameDirectory(game: "CK3" | "EU5") {
    const path = await SelectDirectory(`Select ${game} game directory`);
    if (path) {
      if (game === "CK3") {
        gameInstallPathCk3.set(path);
      } else {
        gameInstallPathEu5.set(path);
      }
    }
  }
</script>

<div class="p-4">
  <div class="max-w-3xl mx-auto w-full">
    <button
      type="button"
      class="btn btn-ghost btn-md gap-2 mb-2 -ml-2 text-base-content/80 hover:text-base-content"
      onclick={goBack}
    >
      ← Back
    </button>
    <h2 class="text-center text-xl font-semibold mb-4 text-base-content/90">
      Settings
    </h2>
    <Card class="bg-base-300 border border-base-content/10">
      <CardBody class="p-6">
        <h3 class="card-title text-base-content/90 mb-2">
          Game install directories
        </h3>
        <p class="text-sm text-base-content/80 mb-4">
          Set the (top-level) install path for each game. These are used by
          Modding Docs, Compare (vanilla vs mod), and Merge (vanilla vs mod).
        </p>
        <div class="flex flex-col gap-4">
          <fieldset class="fieldset">
            <legend class="fieldset-legend text-base-content/90">
              Crusader Kings III (CK3)
            </legend>
            <div class="join">
              <button
                type="button"
                class="btn btn-primary join-item"
                onclick={() => browseGameDirectory("CK3")}
              >
                Browse
              </button>
              <input
                type="text"
                class="input join-item flex-1"
                readonly
                value={$gameInstallPathCk3}
                placeholder="Select CK3 game directory"
              />
            </div>
            <p class="label">
              Steam example: <code
                >C:\Program Files (x86)\Steam\steamapps\common\Crusader Kings
                III</code
              >
            </p>
          </fieldset>
          <fieldset class="fieldset">
            <legend class="fieldset-legend text-base-content/90">
              Europa Universalis V (EU5)
            </legend>
            <div class="join">
              <button
                type="button"
                class="btn btn-primary join-item"
                onclick={() => browseGameDirectory("EU5")}
              >
                Browse
              </button>
              <input
                type="text"
                class="input join-item flex-1"
                readonly
                value={$gameInstallPathEu5}
                placeholder="Select EU5 game directory"
              />
            </div>
            <p class="label">
              Steam example: <code
                >C:\Program Files (x86)\Steam\steamapps\common\Europa
                Universalis V</code
              >
            </p>
          </fieldset>
        </div>
        <div
          class="card-actions justify-end mt-6 pt-4 border-t border-base-content/10"
        >
          <!-- TODO: Add Toast notification for success/error after save -->
          <button type="button" class="btn btn-primary" onclick={save}>
            Save Settings
          </button>
        </div>
      </CardBody>
    </Card>
  </div>
</div>
