<script lang="ts">
  import { Tabs, Tab, MergeTabContent, MergeEditor, MergeHelp, MergeOptionsPanel, MergeResults } from "@components";
  import { createMergeStore, setMergeStore } from "@stores/merge.svelte";
  import { game, helpOpen } from "@stores/app.svelte";

  const store = createMergeStore();
  setMergeStore(store);
  const MERGE_MODES = [
    { id: "vanilla" as const, label: "Vanilla vs mod" },
    { id: "dirs" as const, label: "Two Directories" },
    { id: "pairs" as const, label: "File Pairs" },
  ];

  $effect(() => {
    $game;
    store.reset();
  });
</script>

<div class="p-4 max-w-full min-w-0">
  <MergeHelp bind:open={$helpOpen} />
  <MergeOptionsPanel />

  {#if store.errorMsg}
    <div class="mb-4 p-3 rounded-lg bg-error/20 text-error text-sm border border-error/30">
      {store.errorMsg}
    </div>
  {/if}

  <h3 class="text-sm font-semibold text-base-content/90 mb-3">Choose mode</h3>
  <Tabs class="tabs-border tabs-xl">
    {#each MERGE_MODES as m}
      <Tab
        tabGroup="merge-mode"
        label={m.label}
        selected={store.activeTab === m.id}
        contentClass="bg-base-300 border-base-300 p-6"
        onclick={() => (store.activeTab = m.id)}
      >
        <MergeTabContent mode={m.id} />
      </Tab>
    {/each}
  </Tabs>

  {#if store.mergeResults.length > 0}
    <MergeResults />
  {/if}
</div>

{#if store.currentManualFile}
  <MergeEditor
    fileAPath={store.currentManualFile.pathA}
    fileBPath={store.currentManualFile.pathB}
    relPath={store.currentManualFile.relPath}
    chunks={store.currentManualFile.chunks}
    onSave={(c, s) => store.manualSave(c, s)}
    onAutoMerge={() => store.autoMergeCurrentFile()}
    onSkip={() => store.manualNext()}
    onCancel={() => store.cancelManualMerge()}
  />
{/if}
