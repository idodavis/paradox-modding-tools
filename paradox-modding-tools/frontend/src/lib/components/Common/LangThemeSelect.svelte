<script lang="ts">
  import {
    CODE_LANGUAGES,
    CODE_THEMES,
    codeLanguage,
    codeTheme,
  } from "@stores/monaco.svelte";
</script>

<label class="flex items-center gap-1 text-xs">
  <span class="text-base-content/50">Lang</span>
  <select
    class="select select-xs select-bordered"
    bind:value={$codeLanguage}
    aria-label="Code language"
  >
    {#each CODE_LANGUAGES as l}<option value={l}>{l}</option>{/each}
  </select>
</label>

<label class="flex items-center gap-1 text-xs">
  <span class="text-base-content/50">Theme</span>
  <select
    class="select select-xs select-bordered"
    bind:value={$codeTheme}
    aria-label="Code theme"
  >
    {#each CODE_THEMES as t}<option value={t}>{t}</option>{/each}
  </select>
</label>
