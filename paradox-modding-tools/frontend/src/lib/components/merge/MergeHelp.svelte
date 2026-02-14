<script lang="ts">
  import { Dialog } from "@components";
  let { open = $bindable(false) } = $props();
</script>

<Dialog bind:open size="2xl">
  {#snippet title()}
    <h3 class="text-lg font-bold">How merging works</h3>
  {/snippet}
  {#snippet description()}
    <div class="py-4 text-sm space-y-4">
      <section>
        <h4 class="font-semibold text-base-content mb-2">Vanilla vs mod</h4>
        <p class="text-base-content/90 mb-2">
          Use when merging a vanilla/base game files and a mods files. A =
          vanilla game files, B = mod files.
        </p>
        <ol class="list-decimal list-inside space-y-1 text-base-content/80">
          <li>Set game path in Settings (Modding Docs)</li>
          <li>Select mod folder (B)</li>
          <li>Choose output dir for merged results</li>
        </ol>
      </section>
      <section>
        <h4 class="font-semibold text-base-content mb-2">Two sets</h4>
        <p class="text-base-content/90 mb-2">
          Use when merging two arbitrary file sets (e.g. two mod versions). A
          and B are matched by path or filename.
        </p>
        <ol class="list-decimal list-inside space-y-1 text-base-content/80">
          <li>Select set A (base)</li>
          <li>Select set B</li>
          <li>Choose output dir</li>
        </ol>
      </section>
      <section>
        <h4 class="font-semibold text-base-content mb-2">File Pairs</h4>
        <p class="text-base-content/90 mb-2">
          Explicitly pair files when paths differ. Add pairs A↔B, optional
          output name per pair.
        </p>
      </section>
      <section class="pt-2 border-t border-base-content/10">
        <h4 class="font-semibold text-base-content mb-2">Comment directives</h4>
        <p class="text-base-content/80">
          Add <code class="px-1 py-0.5 rounded bg-base-300"># PREFER:A</code> or
          <code class="px-1 py-0.5 rounded bg-base-300"># USE:B</code> in comments
          above objects to override defaults. Key list forces B for those keys.
        </p>
      </section>
    </div>
  {/snippet}
</Dialog>
