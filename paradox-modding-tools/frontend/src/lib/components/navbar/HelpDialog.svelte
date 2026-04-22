<script lang="ts">
  import { Dialog } from "@components";
  import type { HelpConfig } from "../../config/helpConfig";

  let {
    open = $bindable(false),
    page,
    config,
  } = $props<{
    open?: boolean;
    page: string;
    config: Record<string, HelpConfig>;
  }>();

  const entry = $derived(config[page]);
  const hasContent = $derived(!!entry?.sections?.length);

  function renderInlineCode(text: string): string {
    return text.replace(/`([^`]+)`/g, '<code class="px-1.5 py-0.5 rounded bg-base-300 text-xs">$1</code>');
  }
</script>

<Dialog
  bind:open
  size="2xl"
  overlayProps={{ class: "fixed inset-0 bg-black/70 backdrop-blur-md" }}
  contentProps={{ class: "shadow-2xl ring-2 ring-base-300", onOpenAutoFocus: (e) => e.preventDefault() }}
>
  {#snippet title()}
    <h3 class="text-xl font-bold pt-2">
      {entry?.title ?? "Help"}
    </h3>
  {/snippet}
  {#snippet description()}
    <div class="py-4 text-sm pr-2 space-y-6">
      {#if hasContent}
        {#each entry.sections as section, i (section.heading + String(i))}
          <section class="pb-6">
            <h3 class="text-lg font-semibold text-base-content border-b border-base-content/10 pb-1 mb-2">
              {section.heading}
            </h3>
            <div class="space-y-3">
              {#each section.blocks as block, bi (block.type + String(bi))}
                {#if block.type === "paragraph"}
                  <p class="text-base-content/90 leading-relaxed">
                    {@html renderInlineCode(block.text)}
                  </p>
                {:else if block.type === "list"}
                  <ul class="list-disc list-inside space-y-2 text-base-content/80 leading-relaxed">
                    {#each block.items as item, idx (item + "-" + idx)}
                      <li>{@html renderInlineCode(item)}</li>
                    {/each}
                  </ul>
                {:else if block.type === "orderedList"}
                  <ol class="list-decimal list-inside space-y-2 text-base-content/80 leading-relaxed">
                    {#each block.items as item, idx (item + "-" + idx)}
                      <li>{@html renderInlineCode(item)}</li>
                    {/each}
                  </ol>
                {:else if block.type === "keyValue"}
                  <ul class="space-y-2">
                    {#each block.items as item, ki (item.key + "-" + ki)}
                      <li class="leading-relaxed text-base-content/80">
                        <strong class="text-base-content">{item.key}</strong>
                        — {@html renderInlineCode(item.value)}
                      </li>
                    {/each}
                  </ul>
                {:else if block.type === "listWithSteps"}
                  <ul class="space-y-3">
                    {#each block.items as item, mi (item.main + "-" + mi)}
                      <li class="leading-relaxed text-base-content/80">
                        <span class="font-medium text-base-content">{item.main}</span>
                        {#if item.steps?.length}
                          <ol class="list-decimal list-inside mt-1.5 ml-2 space-y-1">
                            {#each item.steps as step, si (step + "-" + si)}
                              <li>{step}</li>
                            {/each}
                          </ol>
                        {/if}
                      </li>
                    {/each}
                  </ul>
                {:else if block.type === "code"}
                  <pre class="text-xs bg-base-300/80 rounded-md p-3 overflow-x-auto"><code>{block.text}</code></pre>
                {/if}
              {/each}
            </div>
          </section>
        {/each}
      {:else}
        <p class="text-base-content/80">No help available for this page.</p>
      {/if}
    </div>
  {/snippet}
</Dialog>
