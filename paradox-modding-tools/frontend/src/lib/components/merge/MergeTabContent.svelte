<script lang="ts">
  import { Card, CardBody, FileSelector } from "@components";
  import MergePreview from "./MergePreview.svelte";
  import { getMergeStore } from "@stores/merge.svelte";
  import { gameInstallPath } from "@stores/app.svelte";

  type Mode = "vanilla" | "dirs" | "pairs";
  let { mode } = $props<{ mode: Mode }>();
  const store = getMergeStore();
  const CFG: Record<
    Mode,
    {
      whenToUse: string;
      explanation: string;
      canRun: () => boolean;
      run: () => Promise<void>;
      preview?: () => Promise<void>;
      hasPreview: boolean;
    }
  > = {
    vanilla: {
      whenToUse: "Merging a mod into your game.",
      explanation: "A = vanilla game files, B = mod files. Output goes to your chosen directory.",
      canRun: () => store.canRun.vanilla,
      run: () => store.runMerge("vanilla"),
      preview: () => store.runPreview("vanilla"),
      hasPreview: true,
    },
    dirs: {
      whenToUse: "Merging two different script mods or versions of the same mod.",
      explanation:
        "A = base directory, B = other directory. Files are matched by path (or filename if that option is on).",
      canRun: () => store.canRun.dirs,
      run: () => store.runMerge("dirs"),
      preview: () => store.runPreview("dirs"),
      hasPreview: true,
    },
    pairs: {
      whenToUse: "Explicitly pair files when paths differ (e.g. two different mods).",
      explanation: "Add pairs (A↔B), set optional output name per pair. Output dir is the base for all.",
      canRun: () => store.canRun.pairs,
      run: () => store.runPairMerge(),
      hasPreview: false,
    },
  };
  const cfg = $derived(CFG[mode as Mode]);

  const primaryLabel = $derived(
    store.merging
      ? null
      : cfg.hasPreview && store.previewItems.length > 0
        ? "Run Merge"
        : store.previewing
          ? "Previewing..."
          : cfg.hasPreview
            ? "Preview Merge"
            : "Run Merge",
  );
</script>

<Card>
  <CardBody>
    <p class="text-sm text-base-content/90 mb-1">
      <strong>When to use:</strong>
      {cfg.whenToUse}
    </p>
    <p class="text-sm text-base-content/70 mb-4">{cfg.explanation}</p>

    {#if mode === "vanilla"}
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
        initialValue={store.modPath ?? ""}
        onPathChange={(p) => (store.modPath = p ?? "")}
      />
    {:else if mode === "dirs"}
      <FileSelector
        legend="Directory A"
        mode="folder"
        dialogTitle="Directory A"
        btnText="Browse"
        placeholder="A"
        initialValue={store.pathA ?? ""}
        onPathChange={(p) => (store.pathA = p ?? "")}
      />
      <FileSelector
        legend="Directory B"
        mode="folder"
        dialogTitle="Directory B"
        btnText="Browse"
        placeholder="B"
        initialValue={store.pathB ?? ""}
        onPathChange={(p) => (store.pathB = p ?? "")}
      />
    {:else}
      <!-- pairs: output dir first, then pair list -->
    {/if}

    {#if mode !== "pairs"}
      <FileSelector
        legend="Output dir"
        mode="folder"
        dialogTitle="Output dir"
        btnText="Browse"
        placeholder="Output directory"
        initialValue={store.outputDir ?? ""}
        onPathChange={(p) => (store.outputDir = p ?? "")}
      />
      <label class="flex items-center gap-2 cursor-pointer mt-2">
        <input type="checkbox" class="checkbox checkbox-sm" bind:checked={store.rememberOutputDir} />
        <span>Remember output dir</span>
      </label>
    {/if}

    {#if mode === "pairs"}
      <FileSelector
        legend="Output dir"
        mode="folder"
        dialogTitle="Output dir"
        btnText="Browse"
        placeholder="Output directory"
        initialValue={store.outputDir ?? ""}
        onPathChange={(p) => (store.outputDir = p ?? "")}
      />
      <label class="flex items-center gap-2 cursor-pointer mt-2 mb-4">
        <input type="checkbox" class="checkbox checkbox-sm" bind:checked={store.rememberOutputDir} />
        <span>Remember output dir</span>
      </label>
      <div class="space-y-2 mb-4">
        {#each store.filePairs as pair, i}
          <div class="flex flex-wrap items-center gap-2 p-2 rounded border border-base-content/20 bg-base-200/50">
            <FileSelector
              mode="file"
              dialogTitle="File A"
              btnText="A"
              placeholder="A"
              initialValue={pair.pathA ?? ""}
              onPathChange={(p) => store.updatePair(i, "pathA", p ?? "")}
            />
            <span class="text-base-content/50">↔</span>
            <FileSelector
              mode="file"
              dialogTitle="File B"
              btnText="B"
              placeholder="B"
              initialValue={pair.pathB ?? ""}
              onPathChange={(p) => store.updatePair(i, "pathB", p ?? "")}
            />
            <input
              type="text"
              class="input input-bordered input-sm w-40"
              placeholder="Output name (optional)"
              value={pair.outputName}
              oninput={(e) => store.updatePair(i, "outputName", (e.target as HTMLInputElement).value)}
            />
            <button type="button" class="btn btn-ghost btn-xs" onclick={() => store.removePair(i)} title="Remove pair"
              >×</button
            >
          </div>
        {/each}
      </div>
      <div class="flex gap-2">
        <button type="button" class="btn btn-outline btn-secondary btn-sm" onclick={() => store.addPair()}
          >+ Add pair</button
        >
        {#if store.merging}
          <button type="button" class="btn btn-soft btn-wide btn-error" onclick={() => store.cancelMerge()}
            >Cancel</button
          >
        {:else}
          <button
            type="button"
            class="btn btn-soft btn-wide btn-primary"
            disabled={!cfg.canRun()}
            onclick={() => cfg.run()}>Run Merge</button
          >
        {/if}
        <button type="button" class="btn btn-soft btn-ghost" onclick={() => store.reset()}>Clear</button>
      </div>
    {:else}
      <div class="flex flex-wrap gap-2 mt-3">
        {#if store.merging}
          <button type="button" class="btn btn-soft btn-wide btn-error" onclick={() => store.cancelMerge()}
            >Cancel</button
          >
        {:else if store.previewItems.length > 0}
          <button
            type="button"
            class="btn btn-soft btn-wide btn-primary"
            disabled={!cfg.canRun()}
            onclick={() => cfg.run()}>Run Merge</button
          >
        {:else}
          <button
            type="button"
            class="btn btn-soft btn-wide btn-primary"
            disabled={!cfg.canRun() || store.previewing}
            onclick={() => cfg.preview!()}>{primaryLabel}</button
          >
        {/if}
        <button type="button" class="btn btn-soft btn-ghost" onclick={() => store.reset()}>Clear</button>
      </div>
      {#if store.previewItems.length > 0}
        <MergePreview previewItems={store.previewItems} bind:selectedRelPaths={store.selectedRelPaths} />
      {/if}
    {/if}
  </CardBody>
</Card>
