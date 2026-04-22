<script lang="ts">
  import { Dialog } from "@components";

  let {
    open = $bindable(false),
    mode = "save",
    initialName = "",
    onsave,
    oncancel,
  }: {
    open?: boolean;
    mode?: "save" | "rename";
    initialName?: string;
    onsave?: (name: string) => void;
    oncancel?: () => void;
  } = $props();

  let name = $state("");

  $effect(() => {
    if (open) {
      name = initialName;
    }
  });

  function handleSave() {
    const trimmed = name.trim();
    if (trimmed) {
      onsave?.(trimmed);
      open = false;
    }
  }

  function handleCancel() {
    oncancel?.();
    open = false;
  }

  const title = $derived(
    mode === "save" ? "Save inventory" : "Rename inventory",
  );
  const submitLabel = $derived(mode === "save" ? "Save" : "Rename");
</script>

<Dialog bind:open contentProps={{ class: "max-w-md p-6" }}>
  {#snippet title()}{title}{/snippet}
  {#snippet description()}
    {mode === "save"
      ? "Choose a name for this inventory. Unsaved inventories are deleted when you run extraction again or close the app."
      : "Enter a new name for this inventory."}
  {/snippet}
  {#snippet closeDialog()}
    <button type="button" class="btn btn-sm btn-ghost" onclick={handleCancel}>
      Cancel
    </button>
  {/snippet}

  <div class="flex flex-col gap-4 py-4">
    <input
      type="text"
      class="input input-bordered w-full"
      bind:value={name}
      placeholder="Inventory name"
      onkeydown={(e) => e.key === "Enter" && handleSave()}
    />
    <div class="flex justify-end gap-2">
      <button
        type="button"
        class="btn btn-primary"
        disabled={!name.trim()}
        onclick={handleSave}
      >
        {submitLabel}
      </button>
    </div>
  </div>
</Dialog>
