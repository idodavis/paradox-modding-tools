<script lang="ts">
  function clickOutside(node: HTMLElement, onOutside: () => void) {
    function handleClick(e: MouseEvent) {
      if (node && !node.contains(e.target as Node)) onOutside();
    }
    document.addEventListener("click", handleClick, true);
    return {
      destroy() {
        document.removeEventListener("click", handleClick, true);
      },
    };
  }

  type Item = { value: string; label: string };

  let {
    items = [],
    selected = $bindable([] as string[]),
    placeholder = "Select…",
    checkboxColor = "checkbox-success",
    size = "w-48",
    class: className = "",
    disabled = false,
  }: {
    items?: Item[];
    selected?: string[];
    placeholder?: string;
    checkboxColor?:
      | "checkbox-success"
      | "checkbox-primary"
      | "checkbox-secondary";
    size?: string;
    class?: string;
    disabled?: boolean;
  } = $props();

  let open = $state(false);
</script>

<div class={className} use:clickOutside={() => (open = false)}>
  <details class="dropdown" bind:open>
    <summary
      class="select select-bordered cursor-pointer {size}"
      class:disabled
      onclick={(e) => {
        if (disabled) e.preventDefault();
      }}
    >
      {placeholder} ({selected.length} selected)
    </summary>
    <div
      class="dropdown-content {size} max-h-60 overflow-x-hidden overflow-y-auto rounded-box bg-base-100 p-2 shadow z-50"
    >
      <ul
        class="menu flex-nowrap w-full flex flex-col gap-0 bg-transparent p-0"
      >
        {#each items as item}
          <li>
            <label class="label cursor-pointer justify-start gap-2">
              <input
                type="checkbox"
                class="checkbox checkbox-sm {checkboxColor}"
                checked={selected.includes(item.value)}
                onchange={(e) => {
                  const checked = (e.target as HTMLInputElement).checked;
                  if (checked) {
                    selected = [...selected, item.value];
                  } else {
                    selected = selected.filter((v) => v !== item.value);
                  }
                }}
              />
              <span class="label-text">{item.label}</span>
            </label>
          </li>
        {/each}
      </ul>
    </div>
  </details>
</div>
