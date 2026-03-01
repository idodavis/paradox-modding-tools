<script lang="ts">
  import { Dialog, LangThemeSelect, SplitPane } from "@components";
  import DiffView from "../common/DiffView.svelte";
  import EditorView from "../common/EditorView.svelte";
  import {
    getConflictIndices,
    getAddedIndices,
    buildMergedContent,
    computeMergeStats,
  } from "@stores/mergeEditor.svelte";
  import { getMergeStore } from "@stores/merge.svelte";
  import type { MergeConflictChunk } from "@services/models";
  import { showToast } from "@stores/toast.svelte";

  const mergeStore = getMergeStore();

  let { fileAPath, fileBPath, relPath, chunks, onSave, onAutoMerge, onSkip, onCancel } = $props<{
    fileAPath: string;
    fileBPath: string;
    relPath: string;
    chunks: MergeConflictChunk[];
    onSave: (content: string, stats: { changed: number; added: number }) => void;
    onAutoMerge: () => void;
    onSkip: () => void;
    onCancel: () => void;
  }>();

  let resultValues = $state<Record<number, string>>({});
  let resolvedState = $state<Record<number, "A" | "B" | "Custom" | undefined>>({});
  let includedAdditions = $state<Record<number, boolean>>({});
  let editorTab = $state<"conflicts" | "additions">("conflicts");
  let open = $state(true);
  let closing = false;

  const conflictIndices = $derived(getConflictIndices(chunks));
  const addedIndices = $derived(getAddedIndices(chunks));
  const conflictCount = $derived(conflictIndices.length);
  const addedCount = $derived(addedIndices.length);
  const showAdditionsTab = $derived(mergeStore.config.addAdditionalEntries && addedCount > 0);

  $effect(() => {
    relPath;
    chunks;
    if (conflictCount > 0) {
      currentConflictNum = 1;
      editorTab = "conflicts";
    } else if (showAdditionsTab) {
      currentAdditionNum = 1;
      editorTab = "additions";
    }
    if (addedCount > 0) currentAdditionNum = 1;
    if (mergeStore.config.addAdditionalEntries) {
      const next: Record<number, boolean> = {};
      for (const i of addedIndices) next[i] = true;
      includedAdditions = next;
    }
  });

  function stripNL(text: string) {
    const p = text.match(/^(\r?\n)+/)?.[0] ?? "";
    return { body: text.slice(p.length), offset: p.split("\n").length - 1, prefix: p };
  }

  function choose(index: number, side: "A" | "B") {
    const c = chunks[index];
    if (c?.type !== "conflict") return;
    resultValues[index] = side === "A" ? c.textA : c.textB;
    resolvedState[index] = side;
  }

  function chooseRest(side: "A" | "B") {
    for (const idx of conflictIndices) {
      if (!resolvedState[idx]) choose(idx, side);
    }
  }

  const resolvedCount = $derived(Object.values(resolvedState).filter(Boolean).length);
  const unresolvedCount = $derived(conflictCount - resolvedCount);
  const includedCount = $derived(addedIndices.filter((i) => includedAdditions[i]).length);

  function close(action: () => void) {
    closing = true;
    open = false;
    action();
  }
  function save() {
    if (conflictCount > 0 && unresolvedCount > 0) {
      showToast({ message: "Please resolve all conflicts before saving.", type: "alert-warning" });
      return;
    }
    close(
      conflictCount === 0 && addedCount === 0
        ? onAutoMerge
        : () =>
            onSave(
              buildMergedContent(chunks, resultValues, includedAdditions),
              computeMergeStats(chunks, resolvedState, includedAdditions),
            ),
    );
  }

  let currentConflictNum = $state(1);
  let currentAdditionNum = $state(1);

  const currentChunkIndex = $derived(
    currentConflictNum >= 1 && currentConflictNum <= conflictCount
      ? (conflictIndices[currentConflictNum - 1] ?? -1)
      : -1,
  );
  const currentAdditionIndex = $derived(
    currentAdditionNum >= 1 && currentAdditionNum <= addedCount ? (addedIndices[currentAdditionNum - 1] ?? -1) : -1,
  );
  const currentChunk = $derived(currentChunkIndex >= 0 ? chunks[currentChunkIndex] : null);
  const currentAdditionChunk = $derived(currentAdditionIndex >= 0 ? chunks[currentAdditionIndex] : null);
  const currentChoice = $derived(currentChunkIndex >= 0 ? resolvedState[currentChunkIndex] : undefined);

  const diffDisplay = $derived.by(() => {
    if (!currentChunk) return null;
    const a = stripNL(currentChunk.textA),
      b = stripNL(currentChunk.textB);
    return {
      textA: a.body,
      textB: b.body,
      startLineA: currentChunk.startLineA + a.offset,
      startLineB: currentChunk.startLineB + b.offset,
    };
  });

  const resultDisplay = $derived.by(() => {
    if (editorTab === "additions") {
      if (!currentAdditionChunk) return "";
      return includedAdditions[currentAdditionIndex] ? stripNL(currentAdditionChunk.textB).body : "(excluded)";
    }
    if (currentChunkIndex < 0 || !resolvedState[currentChunkIndex]) return "";
    return stripNL(resultValues[currentChunkIndex] ?? "").body;
  });

  const additionsDiffDisplay = $derived.by(() => {
    if (!currentAdditionChunk) return null;
    const b = stripNL(currentAdditionChunk.textB);
    return {
      textA: "(not in A)",
      textB: b.body,
      startLineA: 1,
      startLineB: currentAdditionChunk.startLineB + b.offset,
    };
  });

  function setAdditionIncluded(index: number, value: boolean) {
    includedAdditions = { ...includedAdditions, [index]: value };
  }
  function setAllAdditions(value: boolean) {
    includedAdditions = Object.fromEntries(addedIndices.map((i) => [i, value]));
  }
  function goToAddition(num: number) {
    if (num >= 1 && num <= addedCount) currentAdditionNum = num;
  }

  function onResultChange(value: string) {
    if (currentChunkIndex < 0 || !currentChunk) return;
    resultValues[currentChunkIndex] = stripNL(currentChunk.textA).prefix + value;
    resolvedState[currentChunkIndex] = "Custom";
  }

  function goToConflict(num: number) {
    if (num >= 1 && num <= conflictCount) currentConflictNum = num;
  }

  const choiceBadge = $derived(
    currentChoice === "A"
      ? { cls: "badge-primary", text: `Chose ${mergeStore.labels.a}` }
      : currentChoice === "B"
        ? { cls: "badge-secondary", text: `Chose ${mergeStore.labels.b}` }
        : currentChoice === "Custom"
          ? { cls: "badge-warning", text: "Custom" }
          : { cls: "badge-warning", text: "Unresolved" },
  );
</script>

<Dialog
  bind:open
  size="fullscreen"
  contentProps={{ class: "flex flex-col overflow-hidden !p-0 bg-base-100" }}
  onOpenChange={(o) => {
    if (!o && !closing) onSkip();
  }}
>
  {#snippet title()}
    <div class="px-4 py-2.5 border-b border-base-content/20 bg-base-200/80 flex flex-col gap-1.5 shrink-0">
      <!-- Row 1: Title + Cancel -->
      <div class="flex justify-between items-center">
        <h2 class="text-lg font-semibold truncate min-w-0" title={relPath}>
          Resolving: <span class="text-primary">{relPath}</span>
        </h2>
        <button class="btn btn-sm btn-ghost text-error/70 hover:text-error shrink-0" onclick={() => close(onCancel)}>
          Cancel Merge
        </button>
      </div>

      <!-- Row 2: Tabs (when conflicts or additions) -->
      {#if conflictCount > 0 || showAdditionsTab}
        <div class="flex gap-1">
          <button
            type="button"
            class="btn btn-sm"
            class:btn-primary={editorTab === "conflicts"}
            class:btn-ghost={editorTab !== "conflicts"}
            onclick={() => (editorTab = "conflicts")}
          >
            Conflicts ({conflictCount})
          </button>
          {#if showAdditionsTab}
            <button
              type="button"
              class="btn btn-sm"
              class:btn-primary={editorTab === "additions"}
              class:btn-ghost={editorTab !== "additions"}
              onclick={() => (editorTab = "additions")}
            >
              Additions ({addedCount})
            </button>
          {/if}
        </div>
      {/if}

      <!-- Row 3: Paths + info + layout toggle -->
      <div class="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-base-content/60">
        <span title={fileAPath} class="text-primary/70">{mergeStore.labels.a}: ...{fileAPath.slice(-50)}</span>
        <span title={fileBPath} class="text-secondary/70">{mergeStore.labels.b}: ...{fileBPath.slice(-50)}</span>
        {#if conflictCount > 0}
          <span
            class="badge badge-sm"
            class:badge-success={resolvedCount === conflictCount}
            class:badge-warning={resolvedCount < conflictCount}
          >
            {resolvedCount}/{conflictCount} resolved
          </span>
        {:else if showAdditionsTab}
          <span
            class="badge badge-sm"
            class:badge-success={includedCount === addedCount}
            class:badge-warning={includedCount < addedCount}
          >
            {includedCount}/{addedCount} included
          </span>
        {:else}
          <span class="badge badge-sm badge-info">No conflicts</span>
        {/if}
        <div class="flex items-center gap-3 ml-auto">
          <span class="text-xs text-base-content/50">Result</span>
          <div class="join">
            <button
              type="button"
              class="join-item btn btn-sm btn-outline"
              class:btn-primary={mergeStore.mergeResultLayout === "right"}
              onclick={() => (mergeStore.mergeResultLayout = "right")}
            >
              Right
            </button>
            <button
              type="button"
              class="join-item btn btn-sm btn-outline"
              class:btn-primary={mergeStore.mergeResultLayout === "bottom"}
              onclick={() => (mergeStore.mergeResultLayout = "bottom")}
            >
              Bottom
            </button>
          </div>
          <span class="text-base-content/20">|</span>
          <LangThemeSelect />
        </div>
      </div>

      <!-- Row 4: Skip + Conflict/Additions nav + Save -->
      <div class="flex items-center gap-3 pt-1.5 border-t border-base-content/15">
        <button class="btn btn-sm btn-outline shrink-0" onclick={() => close(onSkip)}>Skip File</button>

        {#snippet prevNext(goTo: (num: number) => void, current: number, total: number, label: string)}
          <div class="join">
            <button
              type="button"
              class="join-item btn btn-sm btn-outline"
              disabled={current <= 1}
              onclick={() => goTo(current - 1)}
            >
              Prev
            </button>
            <button
              type="button"
              class="join-item btn btn-sm btn-outline"
              disabled={current >= total}
              onclick={() => goTo(current + 1)}
            >
              Next
            </button>
          </div>
          <span class="font-medium tabular-nums text-sm text-base-content">{label} {current} of {total}</span>
        {/snippet}

        {#if editorTab === "conflicts" && conflictCount > 0}
          <div class="flex items-center gap-2 flex-1 justify-center flex-wrap">
            {@render prevNext(goToConflict, currentConflictNum, conflictCount, "Conflict")}
            {#if currentChunk}
              <span class="badge badge-sm {choiceBadge.cls}">{choiceBadge.text}</span>
              <span class="text-base-content/20 mx-1">|</span>
              {#if unresolvedCount > 0}
                <button type="button" class="btn btn-xs btn-ghost" onclick={() => chooseRest("A")}>
                  Rest → {mergeStore.labels.a}
                </button>
                <button type="button" class="btn btn-xs btn-ghost" onclick={() => chooseRest("B")}>
                  Rest → {mergeStore.labels.b}
                </button>
                <span class="text-base-content/20">|</span>
              {/if}
              <button
                type="button"
                class="btn btn-sm {currentChoice === 'A' ? 'btn-primary' : 'btn-outline btn-primary'}"
                onclick={() => choose(currentChunkIndex, "A")}
              >
                Choose {mergeStore.labels.a}
              </button>
              <button
                type="button"
                class="btn btn-sm {currentChoice === 'B' ? 'btn-secondary' : 'btn-outline btn-secondary'}"
                onclick={() => choose(currentChunkIndex, "B")}
              >
                Choose {mergeStore.labels.b}
              </button>
            {/if}
          </div>
        {:else if editorTab === "additions" && addedCount > 0}
          <div class="flex items-center gap-2 flex-1 justify-center flex-wrap">
            {@render prevNext(goToAddition, currentAdditionNum, addedCount, "Addition")}
            {#if currentAdditionIndex >= 0}
              <span
                class="badge badge-sm"
                class:badge-success={includedAdditions[currentAdditionIndex]}
                class:badge-ghost={!includedAdditions[currentAdditionIndex]}
              >
                {includedAdditions[currentAdditionIndex] ? "Included" : "Excluded"}
              </span>
              <span class="text-base-content/20 mx-1">|</span>
              <button
                type="button"
                class="btn btn-xs btn-ghost"
                onclick={() => setAdditionIncluded(currentAdditionIndex, !includedAdditions[currentAdditionIndex])}
              >
                {includedAdditions[currentAdditionIndex] ? "Exclude" : "Include"}
              </button>
              <span class="text-base-content/20 mx-1">|</span>
              <button type="button" class="btn btn-xs btn-ghost" onclick={() => setAllAdditions(true)}>
                Include all
              </button>
              <button type="button" class="btn btn-xs btn-ghost" onclick={() => setAllAdditions(false)}>
                Exclude all
              </button>
            {/if}
          </div>
        {:else}
          <div class="flex-1 text-center text-sm text-base-content/60">
            {#if addedCount > 0 && !showAdditionsTab}
              No shared-key conflicts — {addedCount} addition{addedCount > 1 ? "s" : ""} from {mergeStore.labels.b}{" "}
              will be appended
            {:else if addedCount === 0 && conflictCount === 0}
              Files are identical — nothing to merge
            {/if}
          </div>
        {/if}

        <button
          class="btn btn-sm btn-primary shrink-0"
          onclick={save}
          disabled={conflictCount > 0 && resolvedCount < conflictCount}
        >
          Save & Continue
        </button>
      </div>
    </div>
  {/snippet}

  {#snippet description()}<span class="sr-only">Merge Editor</span>{/snippet}

  <div class="flex-1 min-h-0 overflow-hidden">
    {#if (editorTab === "conflicts" && diffDisplay) || (editorTab === "additions" && additionsDiffDisplay)}
      {@const activeDiff = (editorTab === "additions" ? additionsDiffDisplay : diffDisplay)!}
      <SplitPane
        orientation={mergeStore.mergeResultLayout === "right" ? "horizontal" : "vertical"}
        defaultSecondSize={mergeStore.mergeResultLayout === "right" ? 480 : 300}
        fixedSide="second"
        class="h-full rounded-none! border-0!"
      >
        {#snippet first()}
          <DiffView
            originalContent={activeDiff.textA}
            modifiedContent={activeDiff.textB}
            originalLabel={editorTab === "additions" ? "(not in A)" : mergeStore.labels.a}
            modifiedLabel={mergeStore.labels.b}
            origFirstLine={activeDiff.startLineA}
            modFirstLine={activeDiff.startLineB}
            class="h-full"
          />
        {/snippet}
        {#snippet second()}
          <EditorView
            content={resultDisplay}
            label="Result"
            labelClass="bg-accent/10 text-accent"
            firstLineNumber={activeDiff.startLineA}
            readOnly={editorTab === "additions"}
            onContentChange={onResultChange}
            class="h-full"
          />
        {/snippet}
      </SplitPane>
    {:else}
      <div class="flex flex-col items-center justify-center h-full gap-3 text-base-content/50">
        {#if conflictCount === 0 && addedCount > 0}
          <span class="text-lg font-medium">No shared-key conflicts</span>
          <span class="text-sm"
            >{addedCount} entry additions from {mergeStore.labels.b} will be appended to the merged output.</span
          >
          <span class="text-xs text-base-content/30">Click Save & Continue to auto-merge, or Skip File to skip.</span>
        {:else if conflictCount === 0}
          <span class="text-lg font-medium">Files are identical</span>
          <span class="text-sm">No differences found between the two files.</span>
          <span class="text-xs text-base-content/30"
            >Click Save & Continue to write the output, or Skip File to skip.</span
          >
        {:else}
          <span class="text-sm">No conflict selected</span>
        {/if}
      </div>
    {/if}
  </div>
</Dialog>
