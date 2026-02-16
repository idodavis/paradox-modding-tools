<script lang="ts">
  import Icon from "@iconify/svelte";
  import { SelectSingleFile, SelectDirectory } from "@services/fileservice";

  // TODO: Refactor frontend usage of this component as paths are now singular.

  let {
    legend = "File Legend:",
    mode = "file",
    placeholder = "Select File...",
    dialogTitle = "Select File...",
    btnText = "Select File",
    btnColor = "btn-primary",
    fileFilter = "*.txt; *.json",
    underHint = "",
    initialValue = "",
    onPathChange,
  }: {
    legend?: string;
    mode?: "file" | "folder";
    placeholder?: string;
    dialogTitle?: string;
    btnText?: string;
    btnColor?: string;
    fileFilter?: string;
    underHint?: string;
    initialValue?: string;
    onPathChange?: (path: string) => void;
  } = $props();

  let selectedPath = $state<string>("");

  $effect(() => {
    if (initialValue.length > 0) {
      selectedPath = initialValue;
    }
  });

  async function selectSingleFile() {
    selectedPath = await SelectSingleFile(dialogTitle, fileFilter);
    onPathChange?.(selectedPath);
  }

  async function selectSingleDirectory() {
    selectedPath = await SelectDirectory(dialogTitle);
    onPathChange?.(selectedPath);
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
        value={selectedPath}
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
        value={selectedPath}
        {placeholder}
      />
    </div>
    {#if underHint !== ""}
      <p class="label">{underHint}</p>
    {/if}
  {/if}
</fieldset>
