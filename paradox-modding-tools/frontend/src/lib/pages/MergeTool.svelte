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
  import { game, gameInstallPathCk3, gameInstallPathEu5 } from "@stores/app";
  import {
    MergeVanillaMod,
    MergeMultipleFileSets,
    MergeTwoFilesAndSave,
  } from "@services/mergeservice";
  import type { FileMergeResult, MergerOptions } from "@services/models";

  const gameInstallPath = $derived(
    $game === "CK3" ? $gameInstallPathCk3 : $gameInstallPathEu5,
  );

  let pathsA = $state<string[]>([]);
  let pathsB = $state<string[]>([]);
  let modPaths = $state<string[]>([]);
  let fileAPath = $state("");
  let fileBPath = $state("");
  let outputDir = $state("");
  let addAdditionalEntries = $state(true);
  let entryPlacement = $state("bottom");
  let useKeyList = $state(false);
  let customKeys = $state("");
  let commentPrefix = $state("");
  let merging = $state(false);
  let mergeResults = $state<FileMergeResult[]>([]);
  let selectedForDiff = $state<{ pathA: string; pathB: string } | null>(null);
  let mergePromise = $state<
    (Promise<unknown> & { cancel?: () => void }) | null
  >(null);

  const options = $derived.by(
    (): MergerOptions => ({
      addAdditionalEntries,
      entryPlacement,
      keyList: useKeyList
        ? customKeys
            .split(/\r?\n/)
            .map((s) => s.trim())
            .filter(Boolean)
        : [],
      customCommentPrefix: commentPrefix,
    }),
  );

  const canRun = $derived({
    vanilla: !!gameInstallPath?.trim() && modPaths.length > 0 && !!outputDir,
    sets: pathsA.length > 0 && pathsB.length > 0 && !!outputDir,
    any: !!fileAPath && !!fileBPath,
  });

  function cancelMerge() {
    if (mergePromise?.cancel) {
      mergePromise.cancel();
    }
    mergePromise = null;
  }

  async function runMerge(mode: "vanilla" | "sets" | "any") {
    if (!canRun[mode]) return;
    merging = true;
    mergeResults = [];
    mergePromise = null;
    try {
      if (mode === "any") {
        mergePromise = MergeTwoFilesAndSave(fileAPath, fileBPath, options);
        const path = await mergePromise;
        if (path) alert("Saved to: " + path);
      } else {
        mergePromise =
          mode === "vanilla"
            ? MergeVanillaMod(
                $game,
                gameInstallPath!.trim(),
                modPaths,
                outputDir,
                options,
              )
            : MergeMultipleFileSets(pathsA, pathsB, outputDir, options);
        const res = (await mergePromise) as
          | FileMergeResult[]
          | null
          | undefined;
        mergeResults = res ?? [];
        if (!mergeResults.length) alert("No matching files.");
      }
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      if (!msg.toLowerCase().includes("cancel")) {
        alert("Error: " + msg);
      }
    } finally {
      merging = false;
      mergePromise = null;
    }
  }

  const resultColumns = $derived([
    { field: "filePath", headerName: "File", flex: 2 },
    {
      field: "outputPath",
      headerName: "Output",
      flex: 2,
      valueFormatter: (p: { value: string }) =>
        p.value?.split(/[/\\]/).pop() ?? "",
    },
    {
      headerName: "Diffs",
      flex: 2,
      cellRenderer: (params: { data: FileMergeResult }) => {
        const d = params.data;
        const set = (pathA: string, pathB: string) => {
          if (pathA === pathB) alert("Use a different output directory.");
          else selectedForDiff = { pathA, pathB };
        };
        const div = document.createElement("div");
        div.className = "flex gap-2";
        const a = document.createElement("button");
        a.className = "btn btn-sm btn-primary";
        a.textContent = "A vs Out";
        a.onclick = () => set(d.fileAPath, d.outputPath);
        const b = document.createElement("button");
        b.className = "btn btn-sm btn-secondary";
        b.textContent = "B vs Out";
        b.onclick = () => set(d.fileBPath, d.outputPath);
        div.append(a, b);
        return div;
      },
    },
  ]);
</script>

<div class="p-4">
  <details class="group rounded-lg border border-base-content/20 mb-4">
    <summary
      class="px-3 py-2 cursor-pointer text-sm flex items-center justify-between bg-base-200"
    >
      <span class="font-medium">Merge options</span>
      <span class="group-open:rotate-180">▾</span>
    </summary>
    <div
      class="px-3 py-3 text-sm border-t border-base-content/20 space-y-3 bg-base-100"
    >
      <p>
        <strong>A</strong> wins unless key in list (then <strong>B</strong>).
      </p>
      <label class="flex items-center gap-2 cursor-pointer">
        <input
          type="checkbox"
          class="checkbox checkbox-sm"
          bind:checked={addAdditionalEntries}
        />
        <span>Add entries from B not in A</span>
      </label>
      {#if addAdditionalEntries}
        <div class="ml-4 flex gap-4">
          <label class="flex items-center gap-2 cursor-pointer"
            ><input
              type="radio"
              name="placement"
              class="radio radio-sm"
              value="bottom"
              bind:group={entryPlacement}
            /><span>Bottom</span></label
          >
          <label class="flex items-center gap-2 cursor-pointer"
            ><input
              type="radio"
              name="placement"
              class="radio radio-sm"
              value="preserve_order"
              bind:group={entryPlacement}
            /><span>Preserve order</span></label
          >
        </div>
      {/if}
      <label class="flex items-center gap-2 cursor-pointer">
        <input
          type="checkbox"
          class="checkbox checkbox-sm"
          bind:checked={useKeyList}
        />
        <span>Key list (B overrides A)</span>
      </label>
      {#if useKeyList}
        <textarea
          class="textarea textarea-bordered w-full text-sm"
          rows="2"
          placeholder="key1&#10;key2"
          bind:value={customKeys}
        ></textarea>
      {/if}
      <label class="block"
        ><span class="text-sm font-medium">Comment prefix</span><input
          type="text"
          class="input input-bordered input-sm w-full mt-1"
          placeholder="# MOD:"
          bind:value={commentPrefix}
        /></label
      >
    </div>
  </details>

  <Tabs class="tabs-border tabs-xl">
    <Tab
      tabGroup="merge-mode"
      label="Vanilla vs mod"
      selected
      contentClass="bg-base-300 border-base-300 p-6"
    >
      <Card>
        <CardBody>
          <p class="text-sm text-base-content/80 mb-4">
            A = vanilla, B = mod. Set output dir.
          </p>
          <div class="mb-4">
            <input
              type="text"
              class="input input-bordered w-full max-w-2xl mb-2"
              readonly
              value={gameInstallPath ?? ""}
              placeholder="Game path (Modding Docs / Settings)"
            />
            <p class="label text-xs">{$game}</p>
          </div>
          <FileSelector
            mode="filesAndFolders"
            dialogTitle="Mod (B)"
            fileBtnText="Files"
            folderBtnText="Folders"
            placeholder="B files/folders"
            initialValue={modPaths}
            onPathsChange={(p) => (modPaths = p)}
          />
          <FileSelector
            legend="Output dir"
            mode="folderOnly"
            dialogTitle="Output dir"
            folderBtnText="Browse"
            placeholder="Output directory"
            initialValue={outputDir ? [outputDir] : []}
            onPathsChange={(p) => (outputDir = p[0] ?? "")}
          />
          {#if merging}
            <button
              type="button"
              class="btn btn-soft btn-wide btn-error"
              onclick={cancelMerge}>Cancel</button
            >
          {:else}
            <button
              type="button"
              class="btn btn-soft btn-wide btn-primary"
              disabled={!canRun.vanilla}
              onclick={() => runMerge("vanilla")}>Run Merge</button
            >
          {/if}
        </CardBody>
      </Card>
    </Tab>
    <Tab
      tabGroup="merge-mode"
      label="Two sets"
      contentClass="bg-base-300 border-base-300 p-6"
    >
      <Card>
        <CardBody>
          <p class="text-sm text-base-content/80 mb-4">
            Select A and B sets, then output dir.
          </p>
          <FileSelector
            mode="filesAndFolders"
            dialogTitle="Set A"
            fileBtnText="Files"
            folderBtnText="Folders"
            placeholder="A"
            initialValue={pathsA}
            onPathsChange={(p) => (pathsA = p)}
          />
          <FileSelector
            mode="filesAndFolders"
            dialogTitle="Set B"
            fileBtnText="Files"
            folderBtnText="Folders"
            placeholder="B"
            initialValue={pathsB}
            onPathsChange={(p) => (pathsB = p)}
          />
          <FileSelector
            legend="Output dir"
            mode="folderOnly"
            dialogTitle="Output dir"
            folderBtnText="Browse"
            placeholder="Output directory"
            initialValue={outputDir ? [outputDir] : []}
            onPathsChange={(p) => (outputDir = p[0] ?? "")}
          />
          {#if merging}
            <button
              type="button"
              class="btn btn-soft btn-wide btn-error"
              onclick={cancelMerge}>Cancel</button
            >
          {:else}
            <button
              type="button"
              class="btn btn-soft btn-wide btn-primary"
              disabled={!canRun.sets}
              onclick={() => runMerge("sets")}>Run Merge</button
            >
          {/if}
        </CardBody>
      </Card>
    </Tab>
    <Tab
      tabGroup="merge-mode"
      label="Any two files"
      contentClass="bg-base-300 border-base-300 p-6"
    >
      <Card>
        <CardBody>
          <p class="text-sm text-base-content/80 mb-4">
            Merge two files; save dialog opens.
          </p>
          <FileSelector
            mode="fileOnly"
            dialogTitle="File A"
            fileBtnText="Select"
            placeholder="A"
            initialValue={fileAPath ? [fileAPath] : []}
            onPathsChange={(p) => (fileAPath = p[0] ?? "")}
          />
          <FileSelector
            mode="fileOnly"
            dialogTitle="File B"
            fileBtnText="Select"
            placeholder="B"
            initialValue={fileBPath ? [fileBPath] : []}
            onPathsChange={(p) => (fileBPath = p[0] ?? "")}
          />
          {#if merging}
            <button
              type="button"
              class="btn btn-soft btn-wide btn-error mt-4"
              onclick={cancelMerge}>Cancel</button
            >
          {:else}
            <button
              type="button"
              class="btn btn-soft btn-wide btn-primary mt-4"
              disabled={!canRun.any}
              onclick={() => runMerge("any")}>Merge and save</button
            >
          {/if}
        </CardBody>
      </Card>
    </Tab>
  </Tabs>

  <!-- TODO: Add Loading Skeleton while inventory is being extracted -->
  {#if mergeResults.length > 0}
    <Card class="mt-6">
      <CardBody>
        <h3 class="card-title justify-center mb-4">Results</h3>
        <Grid
          columnDefs={resultColumns}
          rowData={mergeResults}
          className="h-[400px] w-full"
        />
      </CardBody>
    </Card>
  {/if}
</div>

{#if selectedForDiff}
  <DiffViewer
    oldFile={selectedForDiff.pathA}
    newFile={selectedForDiff.pathB}
    onclose={() => (selectedForDiff = null)}
  />
{/if}
