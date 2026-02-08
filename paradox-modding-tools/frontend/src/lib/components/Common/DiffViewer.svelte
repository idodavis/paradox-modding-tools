<script lang="ts">
  import { tick } from "svelte";
  import { DiffModeEnum, DiffView, DiffFile } from "@git-diff-view/svelte";
  import { generateDiffFile } from "@git-diff-view/file";
  import "@git-diff-view/svelte/styles/diff-view.css";
  import { ReadFileContent } from "@services/fileservice";
  import { onMount, onDestroy } from "svelte";

  // TODO: Cleanup and maybe use daisyui modal or something for some of the styling

  let {
    oldFile,
    newFile,
    lang = "nginx",
    highlighting = true,
    onclose,
  }: {
    oldFile: string;
    newFile: string;
    lang?: string;
    highlighting?: boolean;
    onclose?: () => void;
  } = $props();

  let diffFile = $state<DiffFile | null>(null);
  let error = $state<string | null>(null);
  let viewModeState = $state<DiffModeEnum>(DiffModeEnum.Split);
  let searchQuery = $state("");
  let matchCount = $state(0);
  let currentMatchIndex = $state(-1);
  let diffContainerRef = $state<HTMLDivElement | null>(null);
  let searchInputRef = $state<HTMLInputElement | null>(null);

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
    error = null;
    (async () => {
      try {
        const [a, b] = await Promise.all([
          ReadFileContent(oldFile),
          ReadFileContent(newFile),
        ]);
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
      c?.querySelectorAll(`.${HL}`).forEach((el) => el.classList.remove(HL));
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

  const onKey = (e: KeyboardEvent) => {
    if ((e.ctrlKey || e.metaKey) && e.key === "f") {
      e.preventDefault();
      searchInputRef?.focus();
    }
  };
  onMount(() => window.addEventListener("keydown", onKey));
  onDestroy(() => {
    document.body.style.overflow = "";
    window.removeEventListener("keydown", onKey);
  });
</script>

<div
  class="fixed inset-0 z-50 bg-base-300/95 flex flex-col"
  role="dialog"
  aria-modal="true"
  aria-label="Diff Viewer"
  tabindex="-1"
  onclick={(e) => e.target === e.currentTarget && onclose?.()}
  onkeydown={(e) => e.key === "Escape" && onclose?.()}
>
  <div
    class="m-4 flex flex-col min-h-0 flex-1 bg-base-100 rounded-xl border border-base-content/20 overflow-hidden shadow-xl"
  >
    <div
      class="px-4 py-3 border-b border-base-content/20 bg-base-200/80 flex flex-col gap-2"
    >
      <div class="flex justify-between items-center">
        <h2 class="text-lg font-semibold truncate">Diff Viewer</h2>
        <button
          type="button"
          class="btn btn-ghost btn-sm"
          onclick={() => onclose?.()}>Close</button
        >
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
    <div
      bind:this={diffContainerRef}
      class="min-h-0 flex-1 overflow-auto bg-base-100"
    >
      {#if error}<p class="p-4 text-error">Error: {error}</p>
      {:else if diffFile}
        <DiffView
          {diffFile}
          diffViewMode={viewModeState}
          diffViewTheme="dark"
          diffViewHighlight={highlighting}
        />
      {/if}
    </div>
  </div>
</div>

<style>
  :global(.diff-search-highlight),
  :global(.diff-search-highlight td) {
    background-color: rgb(245 158 11 / 0.4) !important;
  }
</style>
