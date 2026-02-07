<script lang="ts">
  import { onMount } from "svelte";
  import { Card, CardBody, FileSelector } from "@components";
  import {
    goBack,
    gameInstallPathCk3,
    gameInstallPathEu5,
    saveSettings,
    loadSettings,
  } from "@stores/app";
  import { showToast } from "@stores/toast";

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
            mode="folderOnly"
            dialogTitle="Select CK3 game directory"
            folderBtnText="Browse"
            placeholder="Select CK3 game directory"
            underHint="Steam example: C:\Program Files (x86)\Steam\steamapps\common\Crusader Kings III"
            initialValue={[$gameInstallPathCk3]}
            onPathsChange={(paths: string[]) =>
              gameInstallPathCk3.set(paths[0])}
          />
          <FileSelector
            mode="folderOnly"
            dialogTitle="Select EU5 game directory"
            folderBtnText="Browse"
            placeholder="Select EU5 game directory"
            underHint="Steam example: C:\Program Files (x86)\Steam\steamapps\common\Europa Universalis V"
            initialValue={[$gameInstallPathEu5]}
            onPathsChange={(paths: string[]) =>
              gameInstallPathEu5.set(paths[0])}
          />
        </div>
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
  </div>
</div>
