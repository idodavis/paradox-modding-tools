<script lang="ts">
  import { onMount } from "svelte";
  import ck3Bg from "@assets/CK3-All_Under_Heaven.jpg";
  import eu5Bg from "@assets/EUV-Release.jpg";
  import { Card, CardBody, Toast } from "@components";
  import Icon from "@iconify/svelte";
  import CompareTool from "@pages/CompareTool.svelte";
  import Settings from "@pages/Settings.svelte";
  import { game, currentPage, gotoPage, loadSettings } from "@stores/app";
  import { GetLatestPatchNotes } from "@services/settingsservice";
  import { OpenURL } from "@services/browserservice";
  import type { LatestPatchNotes } from "@services/models";

  const backgroundImage = $derived($game === "EU5" ? eu5Bg : ck3Bg);
  let latestPatchNotes = $state<Record<string, LatestPatchNotes>>({});

  onMount(() => {
    loadSettings();
    GetLatestPatchNotes("CK3").then((result) => {
      latestPatchNotes["CK3"] = result;
    });
    GetLatestPatchNotes("EU5").then((result) => {
      latestPatchNotes["EU5"] = result;
    });
  });
</script>

<Toast />
<div class="min-h-screen flex flex-col">
  <!-- Navbar (solid, on top of background) -->
  <div class="navbar bg-(--navbar-bg) shadow-sm p-6 z-10">
    <button
      class="navbar-start gap-4 cursor-pointer hover:opacity-80 transition-opacity"
      onclick={() => gotoPage("hub")}
    >
      <div class="btn btn-square btn-ghost size-14">
        <Icon
          icon="logos:adonisjs-icon"
          class="size-14 transition-transform group-hover:scale-115"
        />
      </div>
      <div>
        <h2 class="text-2xl font-bold text-left">Paradox Modding Tools</h2>
        <span class="text-xs text-base-content/70 font-medium uppercase">
          Tools and Utilities for Paradox Mod Developers
        </span>
      </div>
    </button>
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
      <button class="btn btn-square btn-ghost" type="button">
        <Icon icon="mdi:help-circle" class="size-6" />
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
              <h2 class="card-title">Modding Docs</h2>
              <p class="text-sm text-base-content/80">
                Browse script help files (.info / readme.txt) and modding wiki
                for CK3 and EU5.
              </p>
              <div class="card-actions justify-end">
                <button class="btn btn-primary btn-outline btn-sm">Open</button>
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
                <button class="btn btn-primary btn-outline btn-sm">Open</button>
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
                <button class="btn btn-primary btn-outline btn-sm">Open</button>
              </div>
            </CardBody>
          </Card>
        </div>
      </div>

      <!-- Vertical banner: Latest Patch Notes -->

      <Card class="bg-base-300/85 max-w-100 min-h-100 max-h-160 shadow-xl">
        <figure>
          <img src={backgroundImage} alt="Game Wallpaper" class="opacity-85" />
        </figure>
        <CardBody>
          <span class="badge badge-md badge-secondary mb-2"
            >Latest Patch Notes</span
          >
          {#if latestPatchNotes[$game]}
            <h2 class="card-title mb-8">
              {latestPatchNotes[$game].description}
            </h2>
            <p class="label mb-2">
              {latestPatchNotes[$game].title}
            </p>
            <div class="card-actions justify-end">
              <button
                class="btn btn-secondary btn-outline btn-sm"
                onclick={() => OpenURL(latestPatchNotes[$game].url)}
                ><Icon icon="mdi:open-in-new" class="size-4" />Open</button
              >
            </div>
          {:else}
            <p>Loading latest patch notes...</p>
          {/if}
        </CardBody>
      </Card>
    </main>

    <!-- Background layers (behind navbar and content) -->
    <div
      class="absolute inset-0 z-0 bg-cover bg-center opacity-30"
      style="background-image: url({backgroundImage})"
    ></div>
    <div
      class="absolute inset-0 z-0 bg-[radial-gradient(ellipse_at_center,var(--color-base-200)_0%,transparent_65%)]"
    ></div>
  {:else if $currentPage === "compare-tool"}
    <main>
      <CompareTool />
    </main>
  {:else if $currentPage === "settings"}
    <main>
      <Settings />
    </main>
  {/if}
</div>
