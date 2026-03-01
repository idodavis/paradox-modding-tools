<script lang="ts">
  import { Tabs, Tab, MergeTabContent, MergeEditor, MergeOptionsPanel, MergeResults } from "@components";
  import { setMergeStore } from "@stores/merge.svelte";
  import { game } from "@stores/app.svelte";

const store = setMergeStore();
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

<div class="p-4 max-w-full min-w-0 flex flex-col gap-4">
  <MergeOptionsPanel />

  {#if store.errorMsg}
    <div class="p-3 rounded border border-error/30 bg-error/20 text-error text-sm">
      {store.errorMsg}
    </div>
  {/if}

  <section>
    <p class="text-xs font-semibold uppercase tracking-wide text-base-content/50 mb-2">Merge mode</p>
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
  </section>

  {#if store.mergeResults.length > 0}
    <MergeResults />
  {/if}
</div>

{#if store.currentManualFile}
  <MergeEditor
    fileAPath={store.currentManualFile.task.pathA}
    fileBPath={store.currentManualFile.task.pathB}
    relPath={store.currentManualFile.task.relPath}
    chunks={store.currentManualFile.chunks}
    onSave={(c, s) => store.manualSave(c, s)}
    onAutoMerge={() => store.autoMergeCurrentFile()}
    onSkip={() => store.manualNext()}
    onCancel={() => store.cancelManualMerge()}
  />
{/if}
