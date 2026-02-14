<script lang="ts">
  import { onMount } from "svelte";
  import ck3Bg from "@assets/CK3-All_Under_Heaven.jpg";
  import eu5Bg from "@assets/EUV-Release.jpg";
  import { Card, CardBody, Dialog, Toast } from "@components";
  import Icon from "@iconify/svelte";
  import CompareTool from "@pages/CompareTool.svelte";
  import ModdingDocs from "@pages/ModdingDocs.svelte";
  import MergeTool from "@pages/MergeTool.svelte";
  import InventoryTool from "@pages/InventoryTool.svelte";
  import Settings from "@pages/Settings.svelte";
  import {
    game,
    currentPage,
    gotoPage,
    loadSettings,
    helpOpen,
  } from "@stores/app.svelte";
  import { showToast } from "@stores/toast.svelte";
  import { GetLatestPatchNotes } from "@services/steamservice";
  import { OpenURL } from "@services/browserservice";
  import type { LatestPatchNotes } from "@services/models";

  const backgroundImage = $derived($game === "EU5" ? eu5Bg : ck3Bg);
  let latestPatchNotes = $state<Record<string, LatestPatchNotes>>({});
  let patchNotesDialogOpen = $state(false);

  const currentPatchNotes = $derived(latestPatchNotes[$game]);

  const pageTitle = $derived(
    (
      {
        hub: "",
        "compare-tool": "File Compare",
        "modding-docs": "Modding Resources",
        "merge-tool": "Script Merger",
        inventory: "Inventory Explorer",
        settings: "Settings",
      } as Record<string, string>
    )[$currentPage] ?? "Tools",
  );

  onMount(() => {
    loadSettings();
    GetLatestPatchNotes("CK3").then((result) => {
      latestPatchNotes["CK3"] = result;
    });
    GetLatestPatchNotes("EU5").then((result) => {
      latestPatchNotes["EU5"] = result;
    });
    const handleRejection = (event: PromiseRejectionEvent) => {
      const err = event.reason;
      const msg = err instanceof Error ? err.message : String(err);
      // Optional: skip user-initiated cancels (e.g. file dialog)
      if (msg.toLowerCase().includes("cancel")) return;
      showToast({ message: msg, type: "alert-error", duration: 6000 });
      event.preventDefault(); // prevent default console error if desired
    };
    window.addEventListener("unhandledrejection", handleRejection);
    return () =>
      window.removeEventListener("unhandledrejection", handleRejection);
  });
</script>

<Toast />
<div class="min-h-screen flex flex-col">
  <!-- Navbar -->
  <div
    class="navbar bg-base-200 border-b border-base-300 shadow-sm px-6 py-3 z-10"
  >
    <div class="navbar-start gap-4 min-w-0">
      {#if $currentPage === "hub"}
        <div class="flex items-center gap-3">
          <div class="flex items-center justify-center size-10">
            <Icon icon="logos:adonisjs-icon" class="size-10" />
          </div>
          <div>
            <h2 class="text-xl font-bold text-left">Paradox Modding Tools</h2>
            <span class="text-xs text-base-content/70 font-medium uppercase">
              Tools and Utilities for Paradox Mod Developers
            </span>
          </div>
        </div>
      {:else}
        <button
          type="button"
          class="btn btn-ghost btn-sm gap-1.5"
          onclick={() => gotoPage("hub")}
        >
          <Icon icon="mdi:arrow-left" class="size-4" />
          Hub
        </button>
        <span class="text-base-content/50">/</span>
        <h1 class="text-lg font-semibold truncate">{pageTitle}</h1>
      {/if}
    </div>
    <div class="navbar-end gap-2">
      <select class="select select-bordered w-32" bind:value={$game}>
        <option value="CK3">CK3</option>
        <option value="EU5">EU5</option>
      </select>
      <button
        class="btn btn-square btn-ghost"
        type="button"
        onclick={() => gotoPage("settings")}
      >
        <Icon icon="mdi:cog" class="size-6" />
      </button>
      <button
        class="btn btn-square btn-ghost"
        type="button"
        onclick={() => helpOpen.update((v) => !v)}
      >
        <Icon icon="mdi:help-circle" class="size-6 text-accent" />
      </button>
    </div>
  </div>

  {#if $currentPage === "hub"}
    <!-- Main content: centered cards + vertical banner -->
    <main class="flex flex-1 gap-6 max-w-7xl mx-auto items-center z-10">
      <!-- Tool cards column -->
      <div class="flex-1 flex flex-col justify-center">
        <div class="grid gap-4 grid-cols-1 sm:grid-cols-2">
          <Card class="shadow-lg bg-base-300/85">
            <CardBody>
              <h2 class="card-title">Modding Resoruces</h2>
              <p class="text-sm text-base-content/80">
                Browse script help files (.info / readme.txt) and modding wiki
                for CK3 and EU5.
              </p>
              <div class="card-actions justify-end">
                <button
                  class="btn btn-primary btn-outline btn-sm"
                  onclick={() => gotoPage("modding-docs")}>Open</button
                >
              </div>
            </CardBody>
          </Card>
          <Card class="shadow-lg bg-base-300/85">
            <CardBody>
              <h2 class="card-title">File Compare</h2>
              <p class="text-sm text-base-content/80">
                Compare two file sets or directories and view diffs side-by-side
                or in unified format.
              </p>
              <div class="card-actions justify-end">
                <button
                  class="btn btn-primary btn-outline btn-sm"
                  onclick={() => gotoPage("compare-tool")}>Open</button
                >
              </div>
            </CardBody>
          </Card>
          <Card class="shadow-lg bg-base-300/85">
            <CardBody>
              <h2 class="card-title">Script Merger</h2>
              <p class="text-sm text-base-content/80">
                Merge Paradox script files with configurable options.
              </p>
              <div class="card-actions justify-end">
                <button
                  class="btn btn-primary btn-outline btn-sm"
                  onclick={() => gotoPage("merge-tool")}>Open</button
                >
              </div>
            </CardBody>
          </Card>
          <Card class="shadow-lg bg-base-300/85">
            <CardBody>
              <h2 class="card-title">Inventory Explorer</h2>
              <p class="text-sm text-base-content/80">
                Extract and explore game objects from script files. Filter by
                type, view references and more.
              </p>
              <div class="card-actions justify-end">
                <button
                  class="btn btn-primary btn-outline btn-sm"
                  onclick={() => gotoPage("inventory")}>Open</button
                >
              </div>
            </CardBody>
          </Card>
        </div>
      </div>

      <!-- Vertical banner: Latest Patch Notes -->
      <div
        class="cursor-pointer"
        role="button"
        tabindex="0"
        onclick={() => latestPatchNotes[$game] && (patchNotesDialogOpen = true)}
        onkeydown={(e: KeyboardEvent) =>
          e.key === "Enter" &&
          latestPatchNotes[$game] &&
          (patchNotesDialogOpen = true)}
      >
        <Card
          class="bg-base-300/85 max-w-100 min-h-100 max-h-160 shadow-xl hover:bg-base-300/95 transition-colors"
        >
          <figure>
            <img
              src={backgroundImage}
              alt="Game Wallpaper"
              class="opacity-85"
            />
          </figure>
          <CardBody>
            <span class="badge badge-md badge-secondary mb-2"
              >Latest Patch Notes</span
            >
            {#if latestPatchNotes[$game]}
              <h2 class="card-title mb-8">
                {latestPatchNotes[$game].title}
              </h2>
              <p class="label mb-2 truncate">
                {latestPatchNotes[$game].contents}
              </p>
              <div class="card-actions justify-end">
                <button
                  class="btn btn-secondary btn-outline btn-sm"
                  onclick={(e) => {
                    e.stopPropagation();
                    OpenURL(latestPatchNotes[$game].url);
                  }}><Icon icon="mdi:open-in-new" class="size-4" />Open</button
                >
              </div>
            {:else}
              <p>Loading latest patch notes...</p>
            {/if}
          </CardBody>
        </Card>
      </div>

      <!-- Patch notes dialog (BBCode content) -->
      <Dialog
        bind:open={patchNotesDialogOpen}
        size="fullscreen"
        contentProps={{
          class: "z-50 bg-dark-input flex flex-col overflow-auto",
        }}
      >
        {#snippet title()}
          <div
            class="flex justify-between items-center border-b border-base-content/20"
          >
            <h3 class="font-bold truncate text-accent">
              {currentPatchNotes.title}
            </h3>
            <button
              type="button"
              class="btn btn-circle btn-ghost"
              onclick={() => (patchNotesDialogOpen = false)}>✕</button
            >
          </div>
        {/snippet}
        {#snippet description()}
          <div class="prose prose-invert max-w-none py-4">
            {@html currentPatchNotes.contents}
          </div>
          <div class="flex justify-end gap-2">
            <button
              class="btn btn-secondary btn-outline btn-sm"
              onclick={() =>
                currentPatchNotes.url && OpenURL(currentPatchNotes.url)}
            >
              <Icon icon="mdi:open-in-new" class="size-4" /> Open on SteamDB
            </button>
          </div>{/snippet}
      </Dialog>
    </main>

    <!-- Background layers (behind navbar and content) -->
    <div
      class="absolute inset-0 z-0 bg-cover bg-center opacity-55"
      style="background-image: url({backgroundImage})"
    ></div>
    <div
      class="absolute inset-0 z-0 bg-[radial-gradient(ellipse_at_center,var(--color-base-200)_0%,transparent_75%)]"
    ></div>
  {:else}
    <main class="flex-1 min-h-0 overflow-auto">
      {#if $currentPage === "compare-tool"}
        <CompareTool />
      {:else if $currentPage === "modding-docs"}
        <ModdingDocs />
      {:else if $currentPage === "settings"}
        <Settings />
      {:else if $currentPage === "merge-tool"}
        <MergeTool />
      {:else if $currentPage === "inventory"}
        <InventoryTool />
      {/if}
    </main>
  {/if}
</div>
