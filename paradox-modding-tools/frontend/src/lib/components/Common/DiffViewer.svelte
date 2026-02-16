<script lang="ts">
  import { tick } from "svelte";
  import { DiffModeEnum, DiffView, type DiffFile } from "@git-diff-view/svelte";
  import { generateDiffFile } from "@git-diff-view/file";
  import "@git-diff-view/svelte/styles/diff-view.css";
  import { ReadFileContent } from "@services/fileservice";
  import { Dialog } from "@components";

  let {
    oldFile,
    newFile,
    oldFileName: customOldFileName,
    newFileName: customNewFileName,
    lang = "nginx",
    highlighting = true,
    onclose,
  }: {
    oldFile: string;
    newFile: string;
    oldFileName?: string;
    newFileName?: string;
    lang?: string;
    highlighting?: boolean;
    onclose?: () => void;
  } = $props();

  let diffFile = $state<DiffFile | null>(null);
  let filesAreEqual = $state(false);
  let error = $state<string | null>(null);
  let viewModeState = $state<DiffModeEnum>(DiffModeEnum.Split);
  let searchQuery = $state("");
  let matchCount = $state(0);
  let currentMatchIndex = $state(-1);
  let diffContainerRef = $state<HTMLDivElement | null>(null);
  let searchInputRef = $state<HTMLInputElement | null>(null);
  let open = $state(true);

  const oldFileName = $derived(
    customOldFileName ?? oldFile?.split(/[/\\]/).pop() ?? oldFile ?? "",
  );
  const newFileName = $derived(
    customNewFileName ?? newFile?.split(/[/\\]/).pop() ?? newFile ?? "",
  );
  const hasNoDiffs = $derived(
    filesAreEqual || (diffFile !== null && diffFile.diffLineLength === 0),
  );
  const HL = "diff-search-highlight";

  function nav(dir: 1 | -1) {
    if (matchCount === 0) return;
    currentMatchIndex =
      dir === 1
        ? (currentMatchIndex + 1) % matchCount
        : currentMatchIndex <= 0
          ? matchCount - 1
          : currentMatchIndex - 1;
  }

  $effect(() => {
    if (!open) {
      onclose?.();
    }
  });

  $effect(() => {
    error = null;
    diffFile = null;
    filesAreEqual = false;

    (async () => {
      if (!oldFile || !newFile) return;
      try {
        const [a, b] = await Promise.all([
          ReadFileContent(oldFile),
          ReadFileContent(newFile),
        ]);
        if (a === b) {
          filesAreEqual = true;
          return;
        }
        const d = generateDiffFile(oldFile, a, newFile, b, lang, lang);
        d.initTheme("dark");
        d.init();
        d.buildSplitDiffLines();
        d.buildUnifiedDiffLines();
        diffFile = d;
      } catch (e) {
        error = e instanceof Error ? e.message : String(e);
      }
    })();
  });

  $effect(() => {
    if (diffFile) document.body.style.overflow = "hidden";
    return () => {
      document.body.style.overflow = "";
    };
  });

  $effect(() => {
    if (searchQuery) currentMatchIndex = 0;
    else currentMatchIndex = -1;
  });

  $effect(() => {
    const c = diffContainerRef;
    const q = searchQuery.trim().toLowerCase();
    const i = currentMatchIndex;
    tick().then(() => {
      if (!c) return;
      c.querySelectorAll(`.${HL}`).forEach((el) => el.classList.remove(HL));
      if (!c || !q) {
        matchCount = 0;
        return;
      }
      const rows = [...c.querySelectorAll<HTMLElement>("tr.diff-line")].filter(
        (row) => row.textContent?.toLowerCase().includes(q),
      );
      matchCount = rows.length;
      if (rows.length === 0) return;
      currentMatchIndex = Math.min(Math.max(0, i), rows.length - 1);
      const target = rows[currentMatchIndex];
      target.classList.add(HL);
      target.scrollIntoView({ behavior: "smooth", block: "center" });
    });
  });

  function onKey(e: KeyboardEvent) {
    if ((e.ctrlKey || e.metaKey) && e.key === "f") {
      e.preventDefault();
      searchInputRef?.focus();
    }
  }
</script>

<svelte:window onkeydown={onKey} />

<Dialog
  bind:open
  size="fullscreen"
  contentProps={{ class: "flex flex-col overflow-hidden !p-0 bg-base-100" }}
>
  {#snippet title()}
    <div
      class="px-4 py-3 border-b border-base-content/20 bg-base-200/80 flex flex-col gap-2 shrink-0"
    >
      <div class="flex justify-between items-center gap-2">
        <h2 class="text-lg font-semibold truncate">
          Comparing: <span class="text-primary">{oldFileName}</span> ↔
          <span class="text-secondary">{newFileName}</span>
        </h2>
        <div class="flex items-center gap-1">
          <button
            type="button"
            class="btn btn-ghost btn-sm"
            onclick={() => (open = false)}>Close</button
          >
        </div>
      </div>
      <div class="flex flex-wrap items-center gap-2">
        <span class="text-sm text-base-content/70">View:</span>
        <div class="join">
          <button
            type="button"
            class="join-item btn btn-sm"
            class:btn-active={viewModeState === DiffModeEnum.Unified}
            onclick={() => (viewModeState = DiffModeEnum.Unified)}
            >Unified</button
          >
          <button
            type="button"
            class="join-item btn btn-sm"
            class:btn-active={viewModeState === DiffModeEnum.Split}
            onclick={() => (viewModeState = DiffModeEnum.Split)}
            >Side-by-side</button
          >
        </div>
        <input
          bind:this={searchInputRef}
          bind:value={searchQuery}
          type="text"
          class="input input-bordered input-sm min-w-[120px] max-w-xs flex-1"
          placeholder="Search (Ctrl+F)"
          onkeydown={(e) =>
            e.key === "Enter" && (e.preventDefault(), nav(e.shiftKey ? -1 : 1))}
        />
        {#if searchQuery}
          {#if matchCount > 0}<span
              class="text-sm text-base-content/60 tabular-nums"
              >{currentMatchIndex + 1}/{matchCount}</span
            >{/if}
          <button
            type="button"
            class="btn btn-ghost btn-sm"
            onclick={() => {
              searchQuery = "";
              currentMatchIndex = -1;
            }}>Clear</button
          >
          {#if matchCount > 0}<button
              type="button"
              class="btn btn-ghost btn-sm btn-square"
              onclick={() => nav(-1)}
              aria-label="Prev">↑</button
            ><button
              type="button"
              class="btn btn-ghost btn-sm btn-square"
              onclick={() => nav(1)}
              aria-label="Next">↓</button
            >{/if}
        {/if}
      </div>
    </div>
  {/snippet}

  {#snippet description()}
    <span class="sr-only">Diff viewer</span>
  {/snippet}

  <div
    bind:this={diffContainerRef}
    class="min-h-0 flex-1 overflow-auto bg-base-100 relative"
  >
    {#if error}<p class="p-4 text-error">Error: {error}</p>
    {:else if hasNoDiffs}
      <p class="p-8 text-center text-base-content/70">
        No differences between the files.
      </p>
    {:else if diffFile}
      <DiffView
        {diffFile}
        diffViewMode={viewModeState}
        diffViewTheme="dark"
        diffViewHighlight={highlighting}
      />
    {/if}
  </div>
</Dialog>

<style>
  :global(.diff-search-highlight),
  :global(.diff-search-highlight td) {
    background-color: rgb(245 158 11 / 0.4) !important;
  }
</style>
