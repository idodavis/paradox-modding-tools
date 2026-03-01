<script lang="ts">
  type PreviewItem = {
    relPath: string;
    pathA: string;
    pathB: string;
    wouldOverwrite: boolean;
    outputPath?: string;
  };

  let { previewItems, selectedRelPaths = $bindable({}) } = $props<{
    previewItems: PreviewItem[];
    selectedRelPaths: Record<string, boolean>;
  }>();

  function setPreviewSelected(relPath: string, checked: boolean) {
    selectedRelPaths = { ...selectedRelPaths, [relPath]: checked };
  }

  function selectAllPreview(checked: boolean) {
    const next: Record<string, boolean> = {};
    for (const p of previewItems) next[p.relPath] = checked;
    selectedRelPaths = next;
  }
</script>

<div class="mt-4 border border-base-content/20 rounded-lg overflow-hidden bg-base-100">
  <div class="bg-base-200/50 px-3 py-2 flex items-center justify-between border-b border-base-content/10">
    <div class="text-sm font-medium">
      Merge Preview <span class="opacity-60">({previewItems.length} files)</span>
    </div>
    <div class="join">
      <button class="btn btn-xs join-item btn-soft" onclick={() => selectAllPreview(true)}>Select All</button>
      <button class="btn btn-xs join-item btn-soft" onclick={() => selectAllPreview(false)}>None</button>
    </div>
  </div>
  <div class="max-h-60 overflow-y-auto">
    <table class="table table-sm table-pin-rows w-full">
      <thead>
        <tr>
          <th class="w-8 text-center">
            <input
              type="checkbox"
              class="checkbox checkbox-xs"
              checked={previewItems.every((p: PreviewItem) => selectedRelPaths[p.relPath] !== false)}
              indeterminate={previewItems.some((p: PreviewItem) => selectedRelPaths[p.relPath] === false) &&
                previewItems.some((p: PreviewItem) => selectedRelPaths[p.relPath] !== false)}
              onclick={(e) => selectAllPreview(e.currentTarget.checked)}
            />
          </th>
          <th>File Path</th>
          <th class="w-24 text-right">Status</th>
        </tr>
      </thead>
      <tbody>
        {#each previewItems as item}
          <tr
            class="hover:bg-base-200/30 cursor-pointer"
            onclick={(e) => {
              if ((e.target as HTMLElement).tagName !== "INPUT") {
                setPreviewSelected(item.relPath, selectedRelPaths[item.relPath] === false);
              }
            }}
          >
            <td class="text-center">
              <input
                type="checkbox"
                class="checkbox checkbox-xs"
                checked={selectedRelPaths[item.relPath] !== false}
                onchange={(e) => setPreviewSelected(item.relPath, e.currentTarget.checked)}
              />
            </td>
            <td class="font-mono text-xs">{item.relPath}</td>
            <td class="text-right">
              {#if item.wouldOverwrite}
                <span class="badge badge-warning badge-xs">Overwrite</span>
              {:else}
                <span class="badge badge-success badge-outline badge-xs">New</span>
              {/if}
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
</div>
