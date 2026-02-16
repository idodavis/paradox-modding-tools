<script lang="ts">
  import { Dialog, CodeBlock } from "@components";
  import * as MergeService from "@services/mergeservice";
  import type { MergeConflictChunk, MergerOptions } from "@services/models";
  import { onMount } from "svelte";
  import Icon from "@iconify/svelte";
  import { showToast } from "@stores/toast.svelte";
  import { diffLines } from "diff";

  let { fileAPath, fileBPath, relPath, options, onSave, onSkip } = $props<{
    fileAPath: string;
    fileBPath: string;
    relPath: string;
    options: MergerOptions;
    onSave: (
      content: string,
      stats: { changed: number; added: number; removed: number },
    ) => void;
    onSkip: () => void;
  }>();

  let chunks = $state<MergeConflictChunk[]>([]);
  let resultValues = $state<Record<number, string>>({});
  let resolvedState = $state<Record<number, "A" | "B" | "Custom" | undefined>>(
    {},
  );
  let loading = $state(true);
  let error = $state("");
  let open = $state(true);

  const Merge = MergeService as any;

  onMount(async () => {
    try {
      chunks = await Merge.GetMergeConflicts(fileAPath, fileBPath, options);
      // No default selection to force user choice
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  });

  function choose(index: number, side: "A" | "B") {
    const chunk = chunks[index];
    if (chunk.type !== "conflict") return;
    resultValues[index] = side === "A" ? chunk.textA : chunk.textB;
    resolvedState[index] = side;
  }

  function chooseAll(side: "A" | "B") {
    chunks.forEach((c, i) => {
      if (c.type === "conflict") {
        resultValues[i] = side === "A" ? c.textA : c.textB;
        resolvedState[i] = side;
      }
    });
  }

  function save() {
    const unresolved = chunks.some(
      (c, i) => c.type === "conflict" && !resolvedState[i],
    );
    if (unresolved) {
      showToast({
        message: "Please resolve all conflicts before saving.",
        type: "alert-warning",
      });
      return;
    }

    let changed = 0;
    let added = 0;

    const content = chunks
      .map((c, i) => {
        if (c.type === "unchanged") return c.text;
        if (c.type === "added") {
          added++;
          return c.text;
        }
        if (resolvedState[i] !== "A") changed++;
        return resultValues[i] ?? "";
      })
      .join("");
    open = false;
    onSave(content, { changed, added, removed: 0 });
  }

  function skip() {
    open = false;
    onSkip();
  }

  const conflictIndices = $derived(
    chunks
      .map((c, i) => (c.type === "conflict" ? i : -1))
      .filter((i) => i !== -1),
  );
  const conflictCount = $derived(conflictIndices.length);
  const resolvedCount = $derived(Object.keys(resolvedState).length);

  let currentConflictIdx = $state(-1);

  function scrollToConflict(idx: number) {
    if (idx < 0 || idx >= conflictIndices.length) return;
    currentConflictIdx = idx;
    const chunkIndex = conflictIndices[idx];
    const el = document.getElementById(`chunk-start-${chunkIndex}`);
    el?.scrollIntoView({ behavior: "smooth", block: "center" });
  }

  function nextConflict() {
    if (currentConflictIdx < conflictCount - 1) {
      scrollToConflict(currentConflictIdx + 1);
    }
  }

  function prevConflict() {
    if (currentConflictIdx > 0) {
      scrollToConflict(currentConflictIdx - 1);
    }
  }

  // Calculate start lines for each chunk to maintain visual continuity in CodeBlocks
  const lineMeta = $derived.by(() => {
    let lineA = 1;
    let lineB = 1;
    let lineRes = 1;
    const meta: { a: number; b: number; res: number }[] = [];

    for (let i = 0; i < chunks.length; i++) {
      meta.push({ a: lineA, b: lineB, res: lineRes });
      const c = chunks[i];
      if (c.type === "unchanged" || c.type === "added") {
        const linesCount = (
          c.text.endsWith("\n") ? c.text.slice(0, -1) : c.text
        ).split(/\r\n|\r|\n/).length;
        lineA += linesCount;
        lineB += linesCount;
        lineRes += linesCount;
      } else {
        const linesA = (
          c.textA.endsWith("\n") ? c.textA.slice(0, -1) : c.textA
        ).split(/\r\n|\r|\n/).length;
        const linesB = (
          c.textB.endsWith("\n") ? c.textB.slice(0, -1) : c.textB
        ).split(/\r\n|\r|\n/).length;
        const val = resultValues[i] ?? "";
        const linesRes = (val.endsWith("\n") ? val.slice(0, -1) : val).split(
          /\r\n|\r|\n/,
        ).length;

        lineA += linesA;
        lineB += linesB;
        lineRes += linesRes;
      }
    }
    return meta;
  });

  function prepareText(text: string) {
    // Remove trailing newline for display in CodeBlock to avoid extra empty line at bottom of block
    if (text.endsWith("\n")) return text.slice(0, -1);
    return text;
  }

  const diffs = $derived(
    chunks.map((c) => {
      if (c.type === "unchanged" || c.type === "added") return { a: [], b: [] };
      const diff = diffLines(c.textA, c.textB);
      let lineA = 1;
      let lineB = 1;
      const a: number[] = [];
      const b: number[] = [];

      diff.forEach((part) => {
        if (part.added) {
          for (let i = 0; i < (part.count ?? 0); i++) b.push(lineB + i);
          lineB += part.count ?? 0;
        } else if (part.removed) {
          for (let i = 0; i < (part.count ?? 0); i++) a.push(lineA + i);
          lineA += part.count ?? 0;
        } else {
          lineA += part.count ?? 0;
          lineB += part.count ?? 0;
        }
      });
      return { a, b };
    }),
  );
</script>

<Dialog
  bind:open
  size="fullscreen"
  contentProps={{ class: "flex flex-col overflow-hidden !p-0 bg-base-100" }}
  onOpenChange={(o) => !o && onSkip()}
>
  {#snippet title()}
    <div
      class="px-4 py-3 border-b border-base-content/20 bg-base-200/80 flex flex-col gap-2 shrink-0"
    >
      <div class="flex flex-col gap-1">
        <div class="flex justify-between items-center gap-2">
          <div class="flex items-center gap-2 min-w-0">
            <h2 class="text-lg font-semibold truncate" title={relPath}>
              Resolving: <span class="text-primary">{relPath}</span>
            </h2>
          </div>
          <div class="flex items-center gap-2">
            <div class="join mr-2">
              <button
                class="join-item btn btn-sm btn-soft"
                onclick={prevConflict}
                disabled={currentConflictIdx <= 0}
              >
                <Icon icon="mdi:chevron-up" /> Prev
              </button>
              <button
                class="join-item btn btn-sm btn-soft"
                onclick={nextConflict}
                disabled={conflictCount === 0 ||
                  currentConflictIdx >= conflictCount - 1}
              >
                Next <Icon icon="mdi:chevron-down" />
              </button>
            </div>
            <button
              class="btn btn-sm btn-primary"
              onclick={save}
              disabled={resolvedCount < conflictCount}
            >
              Save & Continue
            </button>
            <button class="btn btn-sm btn-ghost" onclick={skip}
              >Skip File</button
            >
          </div>
        </div>
        <div class="text-xs text-base-content/60 flex gap-4 truncate">
          <span title={fileAPath}>A: ...{fileAPath.slice(-50)}</span>
          <span title={fileBPath}>B: ...{fileBPath.slice(-50)}</span>
          <span class="badge badge-sm badge-neutral"
            >{resolvedCount}/{conflictCount} resolved</span
          >
        </div>
      </div>
      <div class="flex gap-2 text-xs">
        <button class="btn btn-xs btn-soft" onclick={() => chooseAll("A")}
          >Accept All A (Vanilla)</button
        >
        <button class="btn btn-xs btn-soft" onclick={() => chooseAll("B")}
          >Accept All B (Mod)</button
        >
      </div>
    </div>
  {/snippet}

  {#snippet description()}
    <span class="sr-only">Merge Editor</span>
  {/snippet}

  <div class="flex-1 overflow-auto bg-base-100">
    {#if loading}
      <div class="flex justify-center items-center h-full">
        <span class="loading loading-spinner loading-lg"></span>
      </div>
    {:else if error}
      <div class="text-error p-4">{error}</div>
    {:else}
      <div
        class="grid grid-cols-3 min-w-[1000px] divide-x divide-base-content/10"
      >
        <div
          class="font-bold text-center py-2 bg-base-200/50 text-xs uppercase tracking-wider sticky top-0 z-10 border-b border-base-content/10"
        >
          A (Vanilla)
        </div>
        <div
          class="font-bold text-center py-2 bg-base-200/50 text-xs uppercase tracking-wider sticky top-0 z-10 border-b border-base-content/10"
        >
          Result
        </div>
        <div
          class="font-bold text-center py-2 bg-base-200/50 text-xs uppercase tracking-wider sticky top-0 z-10 border-b border-base-content/10"
        >
          B (Mod)
        </div>

        {#each chunks as chunk, i}
          {#if chunk.type === "unchanged" || chunk.type === "added"}
            <!-- Unchanged: Show in all 3 columns -->
            <div
              class="col-span-1 border-b border-base-content/5 bg-base-100/50 opacity-60 hover:opacity-100 transition-opacity flex flex-col"
            >
              <CodeBlock
                filename={fileAPath}
                content={prepareText(chunk.text)}
                hideHeader
                startLine={lineMeta[i].a}
                class="!border-0 !bg-transparent flex-1"
                highlightLines={diffs[i].a}
                highlightColor="bg-error/20"
              />
            </div>
            <div
              class="col-span-1 border-b border-base-content/5 bg-base-100/50 opacity-60 hover:opacity-100 transition-opacity flex flex-col"
            >
              <CodeBlock
                filename={fileBPath}
                content={prepareText(chunk.text)}
                hideHeader
                startLine={lineMeta[i].res}
                class="!border-0 !bg-transparent flex-1"
              />
            </div>
            <div
              class="col-span-1 border-b border-base-content/5 bg-base-100/50 opacity-60 hover:opacity-100 transition-opacity flex flex-col"
            >
              <CodeBlock
                filename={`Merge Output`}
                content={prepareText(chunk.text)}
                hideHeader
                startLine={lineMeta[i].b}
                class="!border-0 !bg-transparent flex-1"
              />
            </div>
          {:else}
            <!-- Conflict Row -->
            <div class="contents group conflict-row">
              <!-- A Side -->
              <div
                id="chunk-start-{i}"
                class="relative border-b border-base-content/10 transition-colors flex flex-col"
                class:bg-primary={resolvedState[i] === "A"}
                class:bg-opacity-10={resolvedState[i] === "A"}
              >
                <div
                  class="p-2 flex justify-between items-center bg-base-100/30 border-b border-base-content/5"
                >
                  <span class="text-xs font-bold opacity-50">A (Vanilla)</span>
                  <button
                    class="btn btn-xs btn-primary"
                    class:btn-outline={resolvedState[i] !== "A"}
                    onclick={() => choose(i, "A")}
                  >
                    {resolvedState[i] === "A" ? "Selected" : "Choose A"}
                  </button>
                </div>
                <CodeBlock
                  filename={fileAPath}
                  content={prepareText(chunk.textA)}
                  hideHeader
                  startLine={lineMeta[i].a}
                  class="!border-0 !bg-transparent flex-1"
                  highlightLines={diffs[i].a}
                  highlightColor="bg-error/20"
                />
              </div>

              <!-- Result Side -->
              <div
                class="relative border-b border-base-content/10 bg-base-100 flex flex-col"
              >
                <div
                  class="p-2 flex justify-center items-center bg-base-100/30 border-b border-base-content/5 h-[40px]"
                >
                  <span class="text-xs font-bold opacity-50">Result</span>
                </div>
                {#if resultValues[i] !== undefined}
                  <CodeBlock
                    filename="Result"
                    content={prepareText(resultValues[i])}
                    hideHeader
                    startLine={lineMeta[i].res}
                    class="!border-0 !bg-transparent flex-1"
                  />
                {:else}
                  <div
                    class="flex-1 flex items-center justify-center text-base-content/30 text-sm italic p-4"
                  >
                    Select A or B
                  </div>
                {/if}
              </div>

              <!-- B Side -->
              <div
                class="relative border-b border-base-content/10 transition-colors flex flex-col"
                class:bg-secondary={resolvedState[i] === "B"}
                class:bg-opacity-10={resolvedState[i] === "B"}
              >
                <div
                  class="p-2 flex justify-between items-center bg-base-100/30 border-b border-base-content/5"
                >
                  <span class="text-xs font-bold opacity-50">B (Mod)</span>
                  <button
                    class="btn btn-xs btn-secondary"
                    class:btn-outline={resolvedState[i] !== "B"}
                    onclick={() => choose(i, "B")}
                  >
                    {resolvedState[i] === "B" ? "Selected" : "Choose B"}
                  </button>
                </div>
                <CodeBlock
                  filename={fileBPath}
                  content={prepareText(chunk.textB)}
                  hideHeader
                  startLine={lineMeta[i].b}
                  class="!border-0 !bg-transparent flex-1"
                  highlightLines={diffs[i].b}
                  highlightColor="bg-success/20"
                />
              </div>
            </div>
          {/if}
        {/each}
      </div>
    {/if}
  </div>
</Dialog>

<style>
  /* Force transparency for code blocks inside conflict rows to show selection background */
  :global(.conflict-row .code-block-root),
  :global(.conflict-row .code-block-editor),
  :global(.conflict-row .mockup-code),
  :global(.conflict-row pre) {
    background-color: transparent !important;
  }
</style>
