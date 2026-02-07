<script lang="ts">
  import Icon from "@iconify/svelte";
  import {
    SelectFiles,
    SelectDirectories,
    SelectSingleFile,
    SelectDirectory,
  } from "@services/fileservice";

  let {
    legend = "File Legend:",
    mode = "filesAndFolders",
    placeholder = "Select Files or Folders...",
    dialogTitle = "Select Files or Folders...",
    fileBtnText = "Select File(s)",
    folderBtnText = "Select Folder(s)",
    fileFilter = "*.txt; *.json",
    underHint = "",
    initialValue = [],
    onPathsChange,
  }: {
    legend?: string;
    mode?: "filesAndFolders" | "fileOnly" | "folderOnly";
    placeholder?: string;
    dialogTitle?: string;
    fileBtnText?: string;
    folderBtnText?: string;
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

  async function selectFiles() {
    selectedPaths = selectedPaths.concat(
      await SelectFiles(dialogTitle, fileFilter),
    );
    onPathsChange?.(selectedPaths);
  }

  async function selectSingleFile() {
    selectedPaths[0] = await SelectSingleFile(dialogTitle, fileFilter);
    onPathsChange?.(selectedPaths);
  }

  async function selectDirectories() {
    selectedPaths = selectedPaths.concat(await SelectDirectories(dialogTitle));
    onPathsChange?.(selectedPaths);
  }

  async function selectSingleDirectory() {
    selectedPaths[0] = await SelectDirectory(dialogTitle);
    onPathsChange?.(selectedPaths);
  }

  function clear() {
    selectedPaths = [];
    onPathsChange?.(selectedPaths);
  }
</script>

<fieldset class="fieldset">
  <legend class="fieldset-legend text-base-content/90">{legend}</legend>
  {#if mode === "filesAndFolders"}
    <textarea
      class="textarea w-full max-w-2xl"
      readonly
      value={selectedPaths.join("\n")}
      {placeholder}
    ></textarea>
    <div class="flex flex-wrap gap-2 max-w-2xl w-full">
      <button
        type="button"
        class="btn btn-soft btn-secondary w-39"
        onclick={selectFiles}
      >
        {fileBtnText}
      </button>
      <button
        type="button"
        class="btn btn-soft btn-secondary w-39"
        onclick={selectDirectories}
      >
        {folderBtnText}
      </button>
      <button type="button" class="btn w-25 ml-auto" onclick={clear}>
        Clear
        <Icon icon="mdi:trash-can" class="size-4" />
      </button>
    </div>
    {#if underHint !== ""}
      <p class="label">{underHint}</p>
    {/if}
  {:else if mode === "fileOnly"}
    <div class="join">
      <button
        type="button"
        class="btn btn-soft btn-primary join-item"
        onclick={selectSingleFile}
      >
        {fileBtnText}
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
  {:else if mode === "folderOnly"}
    <div class="join">
      <button
        type="button"
        class="btn btn-soft btn-primary join-item"
        onclick={selectSingleDirectory}
      >
        {folderBtnText}
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
