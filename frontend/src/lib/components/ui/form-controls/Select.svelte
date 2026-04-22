<script lang="ts">
  import Icon from "@iconify/svelte";
  import { Select } from "bits-ui";

  let {
    mode = "single",
    items = [] as ReadonlyArray<SelectItem | string>,
    value = "",
    selected = [] as string[],
    onValueChange,
    placeholder = "Select…",
    showCheck = true,
    scrollable = false,
    searchable = false,
    contentWidth = "content" as "trigger" | "content",
    disabled = false,
    triggerClass = "select select-bordered select-sm",
    contentClass = "z-50 rounded-md bg-base-100 border border-base-300 p-2 shadow-lg",
    itemClass = "flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-base-200 data-highlighted:bg-base-200",
    trigger,
    item,
  }: {
    mode?: "single" | "multiple";
    items?: ReadonlyArray<SelectItem | string>;
    value?: string;
    selected?: string[];
    onValueChange?: (v: string | string[]) => void;
    placeholder?: string;
    showCheck?: boolean;
    scrollable?: boolean;
    searchable?: boolean;
    contentWidth?: "trigger" | "content";
    disabled?: boolean;
    triggerClass?: string;
    contentClass?: string;
    itemClass?: string;
    trigger?: import("svelte").Snippet<[selected: SelectItem | undefined, selectedMultiple: SelectItem[]]>;
    item?: import("svelte").Snippet<[item: SelectItem, selected: boolean]>;
  } = $props();

  type SelectItem = { value: string; label: string };

  const norm = (items: ReadonlyArray<SelectItem | string>) =>
    items.map((i) => (typeof i === "string" ? { value: i, label: i } : i));

  const normItems = $derived(norm(items));
  let searchQuery = $state("");
  const filtered = $derived(
    searchable && searchQuery.trim()
      ? normItems.filter((i) => i.label.toLowerCase().includes(searchQuery.toLowerCase().trim()))
      : normItems,
  );
  const selItem = $derived(normItems.find((i) => i.value === value));
  const selItems = $derived(normItems.filter((i) => selected.includes(i.value)));
  const contentCls = $derived(
    contentClass +
      (contentWidth === "trigger"
        ? " w-[var(--bits-select-anchor-width)] min-w-[var(--bits-select-anchor-width)] max-w-[var(--bits-select-anchor-width)]"
        : ""),
  );
  const viewCls = $derived(scrollable ? "max-h-60 overflow-y-auto overflow-x-hidden" : "");
  const label = $derived(
    mode === "single" ? (selItem?.label ?? value) : selItems.length ? `${selItems.length} selected` : placeholder,
  );
  const resetSearch = () => (searchQuery = "");
</script>

{#snippet body()}
  <Select.Trigger class={triggerClass}>
    {#if trigger}
      {@render trigger(mode === "single" ? selItem : undefined, selItems)}
    {:else}
      <span class="truncate">{label}</span>
    {/if}
  </Select.Trigger>
  <Select.Portal>
    <Select.Content class={contentCls} sideOffset={4}>
      {#if searchable}
        <input
          type="text"
          placeholder="Search…"
          class="input input-bordered input-sm mb-2 w-full"
          bind:value={searchQuery}
          onclick={(e) => e.stopPropagation()}
          onkeydown={(e) => e.stopPropagation()}
        />
      {/if}
      <Select.Viewport class={viewCls}>
        {#each filtered as it (it.value)}
          <Select.Item value={it.value} label={it.label} class={itemClass}>
            {#snippet children(p)}
              {#if item}
                {@render item(it, p.selected)}
              {:else}
                <span class="flex flex-1 items-center gap-2">
                  <span>{it.label}</span>
                  {#if showCheck && p.selected}
                    <Icon icon="mdi:check" class="ml-auto size-4 text-accent" />
                  {/if}
                </span>
              {/if}
            {/snippet}
          </Select.Item>
        {/each}
      </Select.Viewport>
    </Select.Content>
  </Select.Portal>
{/snippet}

{#if mode === "single"}
  <Select.Root
    type="single"
    {value}
    onValueChange={(v) => v !== undefined && onValueChange?.(v)}
    items={[...normItems]}
    onOpenChange={(o) => !o && resetSearch()}
    {disabled}
  >
    {@render body()}
  </Select.Root>
{:else}
  <Select.Root
    type="multiple"
    value={selected}
    onValueChange={(v) => onValueChange?.(v ?? [])}
    onOpenChange={(o) => !o && resetSearch()}
    {disabled}
  >
    {@render body()}
  </Select.Root>
{/if}
