<script lang="ts">
  import { LANGUAGE_OPTIONS, THEME_OPTIONS, codeLanguage, codeTheme } from "@stores/code-editor.svelte";
</script>

<label class="flex items-center gap-1 text-xs">
  <span class="text-base-content/50">Lang</span>
  <select class="select select-xs select-bordered" bind:value={$codeLanguage} aria-label="Code language">
    {#each LANGUAGE_OPTIONS as lang (lang.id)}
      <option value={lang.id}>{lang.label}</option>
    {/each}
  </select>
</label>

<label class="flex items-center gap-1 text-xs">
  <span class="text-base-content/50">Theme</span>
  <select class="select select-xs select-bordered" bind:value={$codeTheme} aria-label="Code theme">
    {#each THEME_OPTIONS as theme (theme.id)}
      <option value={theme.id}>{theme.label}</option>
    {/each}
  </select>
</label>
