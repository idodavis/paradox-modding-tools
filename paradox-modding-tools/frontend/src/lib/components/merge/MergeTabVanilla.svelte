<script lang="ts">
  import { Card, CardBody, FileSelector } from "@components";
  import MergePreview from "./MergePreview.svelte";
  import { getMergeStore } from "@stores/merge.svelte";
  import { gameInstallPath } from "@stores/app.svelte";

  const store = getMergeStore();
</script>

<Card>
  <CardBody>
    <p class="text-sm text-base-content/90 mb-1">
      <strong>When to use:</strong> Merging a mod into your game.
    </p>
    <p class="text-sm text-base-content/70 mb-4">
      A = vanilla game files, B = mod files. Output goes to your chosen
      directory.
    </p>
    <input
      type="text"
      class="input input-bordered w-full max-w-2xl mb-2"
      readonly
      value={$gameInstallPath ?? ""}
      placeholder="Game path (Modding Docs / Settings)"
    />
    <FileSelector
      mode="folder"
      legend="Mod folder (B)"
      dialogTitle="Select Mod Folder"
      btnText="Select Folder"
      placeholder="Mod folder (B)"
      initialValue={store.modPaths}
      onPathsChange={(p) => (store.modPaths = p)}
    />
    <FileSelector
      legend="Output dir"
      mode="folder"
      dialogTitle="Output dir"
      btnText="Browse"
      placeholder="Output directory"
      initialValue={store.outputDir ? [store.outputDir] : []}
      onPathsChange={(p) => (store.outputDir = p[0] ?? "")}
    />
    <label class="flex items-center gap-2 cursor-pointer mt-2">
      <input
        type="checkbox"
        class="checkbox checkbox-sm"
        bind:checked={store.rememberOutputDir}
      />
      <span>Remember output dir</span>
    </label>
    <div class="flex flex-wrap gap-2 mt-3">
      {#if store.merging}
        <button
          type="button"
          class="btn btn-soft btn-wide btn-error"
          onclick={() => store.cancelMerge()}>Cancel</button
        >
      {:else if store.previewItems.length > 0}
        <button
          type="button"
          class="btn btn-soft btn-wide btn-primary"
          disabled={!store.canRun.vanilla}
          onclick={() => store.runMerge("vanilla")}>Run Merge</button
        >
      {:else}
        <button
          type="button"
          class="btn btn-soft btn-wide btn-primary"
          disabled={!store.canRun.vanilla || store.previewing}
          onclick={() => store.runPreview("vanilla")}
          >{store.previewing ? "Previewing..." : "Preview Merge"}</button
        >
      {/if}
      <button
        type="button"
        class="btn btn-soft btn-ghost"
        onclick={() => store.reset()}>Clear</button
      >
    </div>
    {#if store.previewItems.length > 0}
      <MergePreview
        previewItems={store.previewItems}
        bind:selectedRelPaths={store.selectedRelPaths}
      />
    {/if}
  </CardBody>
</Card>
