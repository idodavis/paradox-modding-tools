<script lang="ts">
  import { onMount } from "svelte";
  import { Card, CardBody, FileSelector } from "@components";
  import {
    goBack,
    gameInstallPathCk3,
    gameInstallPathEu5,
    appSettings,
    saveSettings,
    loadSettings,
  } from "@stores/app.svelte";
  import { showToast } from "@stores/toast.svelte";
  import * as DbService from "@services/dbservice";

  onMount(() => {
    loadSettings();
  });

  async function save() {
    await saveSettings();
    showToast({
      message: "Settings saved.",
      type: "alert-success",
      duration: 3000,
    });
  }

  async function resetData() {
    if (
      !confirm(
        "Reset all data? This will delete inventories, doc cache, and patch notes. Game install paths and constants will be kept.",
      )
    ) {
      return;
    }
    try {
      await DbService.ResetData();
      await loadSettings();
      showToast({
        message: "Data reset complete.",
        type: "alert-success",
        duration: 3000,
      });
    } catch (e) {
      showToast({
        message:
          "Reset failed: " + (e instanceof Error ? e.message : String(e)),
        type: "alert-error",
        duration: 5000,
      });
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
        <div class="space-y-6">
          <!-- TODO: Prevent cancel dialog from clearing the input -->
          <FileSelector
            mode="folder"
            dialogTitle="Select CK3 game directory"
            btnText="Browse"
            placeholder="Select CK3 game directory"
            underHint="Steam example: C:\Program Files (x86)\Steam\steamapps\common\Crusader Kings III"
            initialValue={[$gameInstallPathCk3]}
            onPathsChange={(paths: string[]) =>
              gameInstallPathCk3.set(paths[0])}
          />
          <FileSelector
            mode="folder"
            dialogTitle="Select EU5 game directory"
            btnText="Browse"
            placeholder="Select EU5 game directory"
            underHint="Steam example: C:\Program Files (x86)\Steam\steamapps\common\Europa Universalis V"
            initialValue={[$gameInstallPathEu5]}
            onPathsChange={(paths: string[]) =>
              gameInstallPathEu5.set(paths[0])}
          />
        </div>
        <h3 class="card-title text-base-content/90 mb-2 mt-6">Merge Tool</h3>
        <p class="text-sm text-base-content/80 mb-4">
          Default output directory for merge operations.
        </p>
        <FileSelector
          mode="folder"
          dialogTitle="Default merge output dir"
          btnText="Browse"
          placeholder="Merge output directory"
          initialValue={$appSettings?.mergeOutputDir
            ? [$appSettings.mergeOutputDir]
            : []}
          onPathsChange={(p) =>
            appSettings.update((s) => ({ ...s, mergeOutputDir: p[0] ?? "" }))}
        />
        <div
          class="card-actions justify-end mt-6 pt-4 border-t border-base-content/10"
        >
          <button
            type="button"
            class="btn btn-soft btn-secondary"
            onclick={save}
          >
            Save Settings
          </button>
        </div>
      </CardBody>
    </Card>

    <Card class="mt-6 bg-base-300 border border-base-content/10">
      <CardBody class="p-6">
        <h3 class="card-title text-base-content/90 mb-2 text-error">
          Reset data
        </h3>
        <p class="text-sm text-base-content/80 mb-4">
          Permanently delete all inventories, doc cache, and patch notes. Game
          install paths and app constants are preserved.
        </p>
        <button
          type="button"
          class="btn btn-error btn-outline"
          onclick={resetData}
        >
          Reset all data
        </button>
      </CardBody>
    </Card>
  </div>
</div>
