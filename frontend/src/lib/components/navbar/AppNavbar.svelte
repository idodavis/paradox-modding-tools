<script lang="ts">
  import Icon from "@iconify/svelte";
  import ck3Icon from "@assets/Icon_CK3.png";
  import eu5Icon from "@assets/Icon_EUV.png";
  import { PMTLogo, Select } from "@components";
  import { currentPage, gotoPage, game, appSettings, saveSettings } from "@stores/app.svelte";

  const GAMES = [
    { value: "CK3" as const, label: "CK3" },
    { value: "EU5" as const, label: "EU5" },
  ];
  const GAME_ICONS: Record<string, string> = { CK3: ck3Icon, EU5: eu5Icon };

  const THEMES = ["PMT", "retro", "pastel", "dracula", "luxury", "autumn", "business", "coffee", "dim"] as const;
  const themeLabel = (t: string) => (t === "PMT" ? "PMT" : t.charAt(0).toUpperCase() + t.slice(1));

  let currentTheme = $state("PMT");

  $effect(() => {
    const saved = $appSettings["_global.theme"];
    if (saved && THEMES.includes(saved as (typeof THEMES)[number])) {
      currentTheme = saved;
      document.documentElement.dataset.theme = saved;
    } else {
      document.documentElement.dataset.theme = "PMT";
    }
  });

  function handleThemeChange(v: string | string[]) {
    const theme = typeof v === "string" ? v : v[0];
    if (!theme || !THEMES.includes(theme as (typeof THEMES)[number])) return;
    currentTheme = theme;
    document.documentElement.dataset.theme = theme;
    appSettings.update((s) => ({ ...s, "_global.theme": theme }));
    saveSettings();
  }

  const themeItems = $derived(THEMES.map((t) => ({ value: t, label: themeLabel(t) })));

  const pageTitle = $derived(
    (
      {
        hub: "",
        "compare-tool": "File Compare",
        "modding-docs": "Modding Docs",
        "merge-tool": "Script Merger",
        inventory: "Inventory Explorer",
        settings: "Settings",
      } as Record<string, string>
    )[$currentPage] ?? "Tools",
  );
</script>

<nav class="navbar shrink-0 bg-base-200 border-b border-base-300 shadow-sm px-2 py-2 z-10">
  <div class="navbar-start gap-4 min-w-0">
    {#if $currentPage === "hub"}
      <PMTLogo iconHeight={40} textHeight={50} />
    {:else}
      <button type="button" class="btn btn-ghost btn-sm gap-1.5" onclick={() => gotoPage("hub")}>
        <Icon icon="mdi:arrow-left" class="size-4" />
        Hub
      </button>
      <span class="text-base-content/50">/</span>
      <h1 class="text-lg font-semibold truncate">{pageTitle}</h1>
    {/if}
  </div>
  <div class="navbar-end gap-2">
    <Select
      value={$game}
      items={GAMES}
      onValueChange={(v) => game.set((typeof v === "string" ? v : v[0]) as "CK3" | "EU5")}
      contentWidth="trigger"
      triggerClass="select select-bordered select-sm w-32"
    >
      {#snippet trigger(sel)}
        <span class="flex items-center gap-2 truncate">
          <img src={GAME_ICONS[sel?.value ?? $game] ?? GAME_ICONS[$game]} alt="" class="size-5 shrink-0" />
          <span class="truncate">{sel?.label ?? $game}</span>
        </span>
      {/snippet}
      {#snippet item(it)}
        <img src={GAME_ICONS[it.value]} alt="" class="size-5 shrink-0" />
        {it.label}
      {/snippet}
    </Select>

    <Select
      value={currentTheme}
      items={themeItems}
      onValueChange={handleThemeChange}
      contentWidth="content"
      showCheck
      triggerClass="btn btn-square btn-ghost"
    >
      {#snippet trigger()}
        <Icon icon="mdi:palette" class="size-6 text-accent" />
      {/snippet}
      {#snippet item(it, selected)}
        <span
          data-theme={it.value}
          class="flex flex-col size-6 shrink-0 rounded overflow-hidden border border-base-300"
          title={themeLabel(it.value)}
        >
          <span class="flex flex-1 w-full">
            <span class="flex-1 bg-primary"></span>
            <span class="flex-1 bg-secondary"></span>
          </span>
          <span class="flex-1 w-full bg-base-content-300"></span>
        </span>
        <span>{it.label}</span>
        {#if selected}
          <Icon icon="mdi:check" class="ml-auto size-4 text-accent" />
        {/if}
      {/snippet}
    </Select>

    <button class="btn btn-square btn-ghost" type="button" onclick={() => gotoPage("settings")}>
      <Icon icon="mdi:cog" class="size-6" />
    </button>
  </div>
</nav>
