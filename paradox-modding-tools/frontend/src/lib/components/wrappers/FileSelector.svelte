<script lang="ts">
  import Icon from "@iconify/svelte";
  import { SelectSingleFile, SelectDirectory } from "@services/fileservice";

  let {
    legend = "File Legend:",
    mode = "file",
    placeholder = "Select File...",
    dialogTitle = "Select File...",
    btnText = "Select File",
    btnColor = "btn-primary",
    fileFilter = "*.txt; *.json",
    underHint = "",
    initialValue = [],
    onPathsChange,
  }: {
    legend?: string;
    mode?: "file" | "folder";
    placeholder?: string;
    dialogTitle?: string;
    btnText?: string;
    btnColor?: string;
    fileFilter?: string;
    underHint?: string;
    initialValue?: string[];
    onPathsChange?: (paths: string[]) => void;
  } = $props();

  let selectedPaths = $state<string[]>([]);

  $effect(() => {
    if (initialValue.length > 0) {
      selectedPaths = [...initialValue];
    }
  });

  async function selectSingleFile() {
    selectedPaths[0] = await SelectSingleFile(dialogTitle, fileFilter);
    onPathsChange?.(selectedPaths);
  }

  async function selectSingleDirectory() {
    selectedPaths[0] = await SelectDirectory(dialogTitle);
    onPathsChange?.(selectedPaths);
  }
</script>

<fieldset class="fieldset">
  <legend class="fieldset-legend text-base-content/90">{legend}</legend>
  {#if mode === "file"}
    <div class="join">
      <button
        type="button"
        class="btn btn-soft {btnColor} join-item"
        onclick={selectSingleFile}
      >
        <Icon icon="mdi:file-edit-outline" class="w-4 h-4 mr-1" />
        {btnText}
      </button>
      <input
        type="text"
        class="input join-item flex-1"
        readonly
        value={selectedPaths[0]}
        {placeholder}
      />
    </div>
    {#if underHint !== ""}
      <p class="label">{underHint}</p>
    {/if}
  {:else if mode === "folder"}
    <div class="join">
      <button
        type="button"
        class="btn btn-soft {btnColor} join-item"
        onclick={selectSingleDirectory}
      >
        <Icon icon="mdi:folder-outline" class="w-4 h-4 mr-1" />
        {btnText}
      </button>
      <input
        type="text"
        class="input join-item flex-1"
        readonly
        value={selectedPaths[0]}
        {placeholder}
      />
    </div>
    {#if underHint !== ""}
      <p class="label">{underHint}</p>
    {/if}
  {/if}
</fieldset>
