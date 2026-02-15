<script lang="ts">
  import { Card, CardBody, FileSelector } from "@components";
  import { getMergeStore } from "@stores/merge.svelte";

  const store = getMergeStore();
</script>

<Card>
  <CardBody>
    <p class="text-sm text-base-content/90 mb-1">
      <strong>When to use:</strong> Explicitly pair files when paths differ (e.g.
      two different mods).
    </p>
    <p class="text-sm text-base-content/70 mb-4">
      Add pairs (A↔B), set optional output name per pair. Output dir is the
      base for all.
    </p>
    <FileSelector
      legend="Output dir"
      mode="folder"
      dialogTitle="Output dir"
      btnText="Browse"
      placeholder="Output directory"
      initialValue={store.outputDir ? [store.outputDir] : []}
      onPathsChange={(p) => (store.outputDir = p[0] ?? "")}
    />
    <label class="flex items-center gap-2 cursor-pointer mt-2 mb-4">
      <input
        type="checkbox"
        class="checkbox checkbox-sm"
        bind:checked={store.rememberOutputDir}
      />
      <span>Remember output dir</span>
    </label>
    <div class="space-y-2 mb-4">
      {#each store.filePairs as pair, i}
        <div
          class="flex flex-wrap items-center gap-2 p-2 rounded border border-base-content/20 bg-base-200/50"
        >
          <FileSelector
            mode="file"
            dialogTitle="File A"
            btnText="A"
            placeholder="A"
            initialValue={pair.pathA ? [pair.pathA] : []}
            onPathsChange={(p) => store.updatePair(i, "pathA", p[0] ?? "")}
          />
          <span class="text-base-content/50">↔</span>
          <FileSelector
            mode="file"
            dialogTitle="File B"
            btnText="B"
            placeholder="B"
            initialValue={pair.pathB ? [pair.pathB] : []}
            onPathsChange={(p) => store.updatePair(i, "pathB", p[0] ?? "")}
          />
          <input
            type="text"
            class="input input-bordered input-sm w-40"
            placeholder="Output name (optional)"
            value={pair.outputName}
            oninput={(e) =>
              store.updatePair(
                i,
                "outputName",
                (e.target as HTMLInputElement).value,
              )}
          />
          <button
            type="button"
            class="btn btn-ghost btn-xs"
            onclick={() => store.removePair(i)}
            title="Remove pair">×</button
          >
        </div>
      {/each}
    </div>
    <div class="flex gap-2">
      <button
        type="button"
        class="btn btn-outline btn-secondary btn-sm"
        onclick={() => store.addPair()}>+ Add pair</button
      >
      {#if store.merging}
        <button
          type="button"
          class="btn btn-soft btn-wide btn-error"
          onclick={() => store.cancelMerge()}>Cancel</button
        >
      {:else}
        <button
          type="button"
          class="btn btn-soft btn-wide btn-primary"
          disabled={!store.canRun.pairs}
          onclick={() => store.runPairMerge()}>Run Merge</button
        >
      {/if}
      <button
        type="button"
        class="btn btn-soft btn-ghost"
        onclick={() => store.reset()}>Clear</button
      >
    </div>
  </CardBody>
</Card>
