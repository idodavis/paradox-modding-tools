<script lang="ts">
  import { Card, CardBody, FileSelector } from "@components";
  import MergePreview from "./MergePreview.svelte";
  import { getMergeStore } from "@stores/merge.svelte";

  const store = getMergeStore();
</script>

<Card>
  <CardBody>
    <p class="text-sm text-base-content/90 mb-1">
      <strong>When to use:</strong> Comparing two mod versions or any two file sets.
    </p>
    <p class="text-sm text-base-content/70 mb-4">
      A = base set, B = other set. Files are matched by path (or filename if
      that option is on).
    </p>
    <FileSelector
      legend="Set A"
      mode="folder"
      dialogTitle="Set A"
      btnText="Browse"
      placeholder="A"
      initialValue={store.pathA ?? ""}
      onPathChange={(p) => (store.pathA = p)}
    />
    <FileSelector
      legend="Set B"
      mode="folder"
      dialogTitle="Set B"
      btnText="Browse"
      placeholder="B"
      initialValue={store.pathB ?? ""}
      onPathChange={(p) => (store.pathB = p)}
    />
    <FileSelector
      legend="Output dir"
      mode="folder"
      dialogTitle="Output dir"
      btnText="Browse"
      placeholder="Output directory"
      initialValue={store.outputDir ? store.outputDir : ""}
      onPathChange={(p) => (store.outputDir = p ?? "")}
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
          onclick={store.cancelMerge}>Cancel</button
        >
      {:else if store.previewItems.length > 0}
        <button
          type="button"
          class="btn btn-soft btn-wide btn-primary"
          disabled={!store.canRun}
          onclick={() => store.runMerge("dirs")}>Run Merge</button
        >
      {:else}
        <button
          type="button"
          class="btn btn-soft btn-wide btn-primary"
          disabled={!store.canRun || store.previewing}
          onclick={() => store.runPreview("dirs")}
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
