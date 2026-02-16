<script lang="ts">
  import {
    Tabs,
    Tab,
    DiffViewer,
    MergeTabVanilla,
    MergeTabDirs,
    MergeTabPairs,
    MergeEditor,
    MergeHelp,
    MergeOptionsPanel,
    MergeResults,
  } from "@components";

  import { createMergeStore, setMergeStore } from "@stores/merge.svelte";
  import { game, helpOpen } from "@stores/app.svelte";

  const store = createMergeStore();
  setMergeStore(store);

  $effect(() => {
    $game;
    store.reset();
  });
</script>

<div class="p-4 max-w-full min-w-0">
  <MergeHelp bind:open={$helpOpen} />
  <MergeOptionsPanel />

  {#if store.errorMsg}
    <div
      class="mb-4 p-3 rounded-lg bg-error/20 text-error text-sm border border-error/30"
    >
      {store.errorMsg}
    </div>
  {/if}

  <h3 class="text-sm font-semibold text-base-content/90 mb-3">Choose mode</h3>
  <Tabs class="tabs-border tabs-xl">
    <Tab
      tabGroup="merge-mode"
      label="Vanilla vs mod"
      selected
      contentClass="bg-base-300 border-base-300 p-6"
      onclick={() => (store.activeTab = "vanilla")}
    >
      <MergeTabVanilla />
    </Tab>
    <Tab
      tabGroup="merge-mode"
      label="Two sets"
      contentClass="bg-base-300 border-base-300 p-6"
      onclick={() => (store.activeTab = "sets")}
    >
      <MergeTabDirs />
    </Tab>
    <Tab
      tabGroup="merge-mode"
      label="File Pairs"
      contentClass="bg-base-300 border-base-300 p-6"
      onclick={() => (store.activeTab = "pairs")}
    >
      <MergeTabPairs />
    </Tab>
  </Tabs>

  {#if store.mergeResults.length > 0}
    <MergeResults />
  {/if}
</div>

{#if store.selectedForDiff}
  <DiffViewer
    oldFile={store.selectedForDiff.pathA}
    newFile={store.selectedForDiff.pathB}
    onclose={() => (store.selectedForDiff = null)}
  />
{/if}

{#if store.currentManualFile}
  <MergeEditor
    fileAPath={store.currentManualFile.pathA}
    fileBPath={store.currentManualFile.pathB}
    relPath={store.currentManualFile.relPath}
    options={store.options}
    onSave={(c, s) => store.onManualSave(c, s)}
    onSkip={() => store.onManualSkip()}
  />
{/if}
