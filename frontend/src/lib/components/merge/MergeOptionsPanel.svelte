<script lang="ts">
  import { onMount } from "svelte";
  import { FileSelector } from "@components";
  import * as SettingsService from "@services/settingsservice";
  import { showToast } from "@stores/toast.svelte";
  import type { MergerOptions, MergePreset } from "@services/models";
  import { getMergeStore } from "@stores/merge.svelte";
  import { GetUserDownloadsDir } from "@services/fileservice";

  type MergePresetItem = { name: string; options: MergerOptions };

  let defaultOutputDir = $state("");

  const store = getMergeStore();
  const config = store.config;

  let presets = $state<MergePresetItem[]>([]);
  let currentPresetName = $state<string | null>(null);
  let presetNameToSave = $state("");
  let errorMsg = $state("");

  async function loadPresets() {
    try {
      const p = (await SettingsService.GetMergePresets()) ?? [];
      presets = p.map((x: MergePreset) => ({ name: x.name, options: x.options }));

      const last = localStorage.getItem("last_merge_preset");
      const found = last ? presets.find((x) => x.name === last) : null;
      if (found) {
        applyPreset(found);
        currentPresetName = last;
      } else if (!store.outputDir) {
        store.outputDir = defaultOutputDir;
      }
    } catch {
      presets = [];
    }
  }

  function applyPreset(p: MergePresetItem) {
    const o = p.options;
    config.addAdditionalEntries = o.addAdditionalEntries ?? true;
    config.manualConflictResolution = o.manualConflictResolution ?? false;
    config.useKeyList = (o.keyList?.length ?? 0) > 0;
    config.customKeys = (o.keyList ?? []).join("\n");
    config.matchByFilenameOnly = o.matchByFilenameOnly ?? false;
    config.includePathPattern = o.includePathPattern ?? "";
    config.excludePathPattern = o.excludePathPattern ?? "";
    config.outputFileSuffix = o.outputFileSuffix ?? "";
    store.outputDir = o.outputDir?.trim() || defaultOutputDir || "";
    localStorage.setItem("last_merge_preset", p.name);
    currentPresetName = p.name;
  }

  async function saveCurrentPreset() {
    if (!presetNameToSave.trim()) return;
    const options: MergerOptions = {
      manualConflictResolution: config.manualConflictResolution,
      addAdditionalEntries: config.addAdditionalEntries,
      keyList: config.useKeyList
        ? config.customKeys
            .split(/\r?\n/)
            .map((s: string) => s.trim())
            .filter(Boolean)
        : [],
      matchByFilenameOnly: config.matchByFilenameOnly,
      includePathPattern: config.includePathPattern,
      excludePathPattern: config.excludePathPattern,
      outputFileSuffix: config.outputFileSuffix,
      outputDir: store.outputDir ?? "",
    };
    try {
      const name = presetNameToSave.trim();
      await SettingsService.SaveMergePreset(name, options);
      showToast({ message: "Preset saved", type: "alert-success" });
      localStorage.setItem("last_merge_preset", name);
      currentPresetName = name;
      presetNameToSave = "";
      loadPresets();
    } catch (e) {
      errorMsg = e instanceof Error ? e.message : String(e);
    }
  }

  async function deletePreset(name: string) {
    try {
      await SettingsService.DeleteMergePreset(name);
      if (currentPresetName === name) currentPresetName = null;
      showToast({ message: "Preset deleted", type: "alert-success" });
      loadPresets();
    } catch (e) {
      errorMsg = e instanceof Error ? e.message : String(e);
    }
  }

  onMount(async () => {
    defaultOutputDir = (await GetUserDownloadsDir()) ?? "";
    await loadPresets();
  });
</script>

<div class="rounded-lg border border-base-content/10 bg-base-200/30 mb-4 overflow-hidden">
  <!-- Always-visible summary bar -->
  <div class="px-4 py-2 flex flex-wrap items-center gap-2 gap-y-1.5 border-b border-base-content/10 bg-base-200/50">
    <span class="text-xs font-semibold uppercase tracking-wide text-base-content/50 pr-2">Options:</span>
    <span
      class="badge badge-sm"
      class:badge-primary={!config.manualConflictResolution}
      class:badge-secondary={config.manualConflictResolution}
    >
      {config.manualConflictResolution ? "Manual" : "Auto"}
    </span>
    <span class="badge badge-sm badge-ghost">
      Add from B: {config.addAdditionalEntries ? "Yes" : "No"}
    </span>
    <span class="badge badge-sm badge-ghost">
      Preset: {currentPresetName ?? "None"}
    </span>
    <span class="badge badge-sm badge-ghost truncate max-w-48" title={store.outputDir || "Downloads"}>
      Out: {store.outputDir || "Downloads"}
    </span>
  </div>

  <details class="group">
    <summary
      class="px-4 py-2.5 cursor-pointer text-sm flex items-center justify-between font-medium select-none hover:bg-base-200/50 transition-colors border-b border-base-content/10"
    >
      <span class="text-base-content/70">More options</span>
      <span class="group-open:rotate-180 transition-transform text-base-content/50">▾</span>
    </summary>
    <div class="px-3 py-4 text-sm border-t border-base-content/20 space-y-4 bg-base-100">
      <!-- Presets -->
      <div class="pb-2 border-b border-base-content/10 space-y-2">
        <span class="block text-xs font-medium">Presets</span>
        <div class="flex flex-wrap gap-2 items-center">
          {#if currentPresetName === null && presets.length === 0}
            <span class="text-xs text-base-content/50">No preset</span>
          {:else}
            <select
              class="select select-bordered select-sm max-w-[160px]"
              value={currentPresetName ?? ""}
              onchange={(e) => {
                const name = (e.target as HTMLSelectElement).value;
                currentPresetName = name || null;
                const p = presets.find((x) => x.name === name);
                if (p) applyPreset(p);
                else if (!store.outputDir) store.outputDir = defaultOutputDir;
              }}
            >
              <option value="">— No preset —</option>
              {#each presets as p}
                <option value={p.name}>{p.name}</option>
              {/each}
            </select>
          {/if}
          <input
            type="text"
            class="input input-bordered input-sm w-28"
            placeholder="Name"
            bind:value={presetNameToSave}
          />
          <button
            type="button"
            class="btn btn-ghost btn-sm"
            disabled={!presetNameToSave.trim()}
            onclick={saveCurrentPreset}
          >
            Save
          </button>
          {#if presets.length > 0}
            <span class="text-base-content/50 text-xs">Delete:</span>
            {#each presets as p}
              <button
                type="button"
                class="btn btn-ghost btn-xs px-1"
                title={`Delete ${p.name}`}
                onclick={() => deletePreset(p.name)}
              >
                {p.name} ×
              </button>
            {/each}
          {/if}
        </div>
        {#if errorMsg}
          <div class="text-error text-xs">{errorMsg}</div>
        {/if}
      </div>

      <!-- Mode Selection -->
      <div class="pb-4 border-b border-base-content/10">
        <span class="block text-xs font-medium mb-2">Resolution Mode</span>
        <div class="join w-full">
          <input
            class="join-item btn btn-sm flex-1"
            type="radio"
            name="mergeMode"
            aria-label="Auto"
            value={false}
            bind:group={config.manualConflictResolution}
          />
          <input
            class="join-item btn btn-sm flex-1"
            type="radio"
            name="mergeMode"
            aria-label="Manual"
            value={true}
            bind:group={config.manualConflictResolution}
          />
        </div>
        <p class="text-xs text-base-content/60 mt-2">
          {#if config.manualConflictResolution}
            Resolve conflicts manually using a 3-way editor.
          {:else}
            Conflicts resolved automatically (A wins by default).
          {/if}
        </p>
      </div>

      <div>
        <label
          class="flex items-center gap-2 cursor-pointer"
          title="Append entities that exist only in B to the merged output. In Manual mode, these become the Additions tab."
        >
          <input type="checkbox" class="checkbox checkbox-sm" bind:checked={config.addAdditionalEntries} />
          <span>Add entries from B not in A</span>
        </label>
        <p class="text-xs text-base-content/60 mt-1 ml-6">Entities in B that don't exist in A are appended.</p>
      </div>

      <div
        class:opacity-50={config.manualConflictResolution}
        class:pointer-events-none={config.manualConflictResolution}
        title={config.manualConflictResolution
          ? "Key list only affects Auto mode (conflicts are resolved manually in Manual mode)"
          : "For these keys, always use B's version even when A has them"}
      >
        <label class="flex items-center gap-2 cursor-pointer">
          <input
            type="checkbox"
            class="checkbox checkbox-sm"
            bind:checked={config.useKeyList}
            disabled={config.manualConflictResolution}
          />
          <span>Key list (B overrides A)</span>
        </label>
        <p class="text-xs text-base-content/60 mt-1 ml-6">
          One key per line. B wins for these entities. Only applies in Auto mode.
        </p>
      </div>
      {#if config.useKeyList && !config.manualConflictResolution}
        <textarea
          class="textarea textarea-bordered w-full text-sm ml-6"
          rows="2"
          placeholder="key1&#10;key2"
          bind:value={config.customKeys}
        ></textarea>
      {/if}

      <label class="flex items-center gap-2 cursor-pointer">
        <input type="checkbox" class="checkbox checkbox-sm" bind:checked={config.matchByFilenameOnly} />
        <span>Match by filename only</span>
      </label>

      <label class="block">
        <span class="text-sm font-medium">Include path (regex)</span>
        <input
          type="text"
          class="input input-bordered input-sm w-full mt-1"
          placeholder="events/"
          bind:value={config.includePathPattern}
        />
      </label>

      <label class="block">
        <span class="text-sm font-medium">Exclude path (regex)</span>
        <input
          type="text"
          class="input input-bordered input-sm w-full mt-1"
          placeholder="common/"
          bind:value={config.excludePathPattern}
        />
      </label>

      <label class="block">
        <span class="text-sm font-medium">Output file suffix</span>
        <input
          type="text"
          class="input input-bordered input-sm w-full mt-1"
          placeholder="_merged"
          bind:value={config.outputFileSuffix}
        />
      </label>

      <FileSelector
        mode="folder"
        legend="Output directory"
        dialogTitle="Select Output Directory"
        btnText="Browse"
        placeholder="Output directory for merged files"
        initialValue={store.outputDir ?? ""}
        onPathChange={(p) => (store.outputDir = p ?? "")}
      />
    </div>
  </details>
</div>
