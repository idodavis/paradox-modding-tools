<script lang="ts">
  import Icon from "@iconify/svelte";
  import { HelpDialog } from "@components";
  import { helpConfig } from "../config/helpConfig";
  import type { PageId } from "../config/helpConfig";

  const PAGE_TITLES: Record<string, string> = {
    hub: "Hub",
    "compare-tool": "File Compare",
    "modding-docs": "Modding Docs",
    "merge-tool": "Script Merger",
    inventory: "Inventory Explorer",
    settings: "Settings",
  };

  let { page } = $props<{ page: PageId }>();
  let open = $state(false);
  const title = $derived(PAGE_TITLES[page] ?? "Tools");
</script>

<button
  type="button"
  class="btn btn-ghost btn-sm gap-1.5 text-base-content/80 hover:text-accent"
  onclick={() => (open = true)}
>
  <Icon icon="mdi:help-circle" class="size-4" />
  Help for {title}
</button>
<HelpDialog bind:open config={helpConfig} {page} />
