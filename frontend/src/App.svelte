<script lang="ts">
  import { onMount } from "svelte";
  import ck3Bg from "@assets/CK3-All_Under_Heaven.jpg";
  import eu5Bg from "@assets/EUV-Release.jpg";
  import { Card, CardBody, Dialog, Toast, PageHelp, AppNavbar, PMTLogo } from "@components";
  import Icon from "@iconify/svelte";
  import CompareTool from "@pages/CompareTool.svelte";
  import ModdingDocs from "@pages/ModdingDocs.svelte";
  import MergeTool from "@pages/MergeTool.svelte";
  import InventoryTool from "@pages/InventoryTool.svelte";
  import Settings from "@pages/Settings.svelte";
  import { game, currentPage, gotoPage, loadSettings } from "@stores/app.svelte";
  import { LogError } from "@services/logservice";
  import { GetLatestPatchNotes } from "@services/steamservice";
  import { OpenURL } from "@services/browserservice";
  import type { LatestPatchNotes } from "@services/models";
  import pkg from "../package.json";

  const backgroundImage = $derived($game === "EU5" ? eu5Bg : ck3Bg);
  let latestPatchNotes = $state<Record<string, LatestPatchNotes>>({});
  let patchNotesDialogOpen = $state(false);

  const currentPatchNotes = $derived(latestPatchNotes[$game]);

  onMount(() => {
    loadSettings();
    GetLatestPatchNotes("CK3").then((result) => {
      latestPatchNotes["CK3"] = result;
    });
    GetLatestPatchNotes("EU5").then((result) => {
      latestPatchNotes["EU5"] = result;
    });
    const logErr = (msg: string, stack?: string) => LogError(msg, stack ?? "").catch(() => {});
    const handleRejection = (e: PromiseRejectionEvent) => {
      const err = e.reason;
      const msg = err instanceof Error ? err.message : String(err);
      if (msg.toLowerCase().includes("cancel")) return;
      if (/\b(t\.with|monaco|minified|startLineNumber)\b/i.test(msg)) return;
      const stack = err instanceof Error ? err.stack ?? "" : "";
      logErr(msg, stack);
      e.preventDefault();
    };
    const handleError = (e: ErrorEvent) => {
      const msg = e.message ?? String(e);
      logErr(msg, e.error instanceof Error ? e.error.stack ?? "" : "");
    };
    window.addEventListener("unhandledrejection", handleRejection);
    window.addEventListener("error", handleError);
    return () => {
      window.removeEventListener("unhandledrejection", handleRejection);
      window.removeEventListener("error", handleError);
    };
  });
</script>

<!-- Toast uses fixed/portal rendering - must not affect layout -->
<Toast />
<div class="h-screen overflow-hidden flex flex-col">
  <AppNavbar />

  {#if $currentPage === "hub"}
    <!-- Main content: centered cards + vertical banner -->
    <main class="flex flex-1 min-h-0 overflow-auto gap-6 max-w-7xl mx-auto items-center z-10">
      <!-- Tool cards column -->
      <div class="flex-1 flex flex-col justify-center">
        <div class="grid gap-4 grid-cols-1 sm:grid-cols-2">
          <Card class="shadow-lg bg-base-300/85">
            <CardBody>
              <h2 class="card-title">Modding Docs</h2>
              <p class="text-sm text-base-content/80">
                Browse script help files (.info / readme.txt) and modding wiki for CK3 and EU5.
              </p>
              <div class="card-actions justify-end">
                <button class="btn btn-primary btn-outline btn-sm" onclick={() => gotoPage("modding-docs")}>Open</button
                >
              </div>
            </CardBody>
          </Card>
          <Card class="shadow-lg bg-base-300/85">
            <CardBody>
              <h2 class="card-title">File Compare</h2>
              <p class="text-sm text-base-content/80">
                Compare two file sets or directories and view diffs side-by-side or in unified format.
              </p>
              <div class="card-actions justify-end">
                <button class="btn btn-primary btn-outline btn-sm" onclick={() => gotoPage("compare-tool")}>Open</button
                >
              </div>
            </CardBody>
          </Card>
          <Card class="shadow-lg bg-base-300/85">
            <CardBody>
              <h2 class="card-title">Script Merger</h2>
              <p class="text-sm text-base-content/80">Merge Paradox script files with configurable options.</p>
              <div class="card-actions justify-end">
                <button class="btn btn-primary btn-outline btn-sm" onclick={() => gotoPage("merge-tool")}>Open</button>
              </div>
            </CardBody>
          </Card>
          <Card class="shadow-lg bg-base-300/85">
            <CardBody>
              <h2 class="card-title">Inventory Explorer</h2>
              <p class="text-sm text-base-content/80">
                Extract and explore game objects from script files. Filter by type, view references and more.
              </p>
              <div class="card-actions justify-end">
                <button class="btn btn-primary btn-outline btn-sm" onclick={() => gotoPage("inventory")}>Open</button>
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
        onkeydown={(e: KeyboardEvent) => e.key === "Enter" && latestPatchNotes[$game] && (patchNotesDialogOpen = true)}
      >
        <Card class="bg-base-300/85 max-w-100 min-h-100 max-h-160 shadow-xl hover:bg-base-300/95 transition-colors">
          <figure>
            <img src={backgroundImage} alt="Game Wallpaper" class="opacity-85" />
          </figure>
          <CardBody>
            <span class="badge badge-md badge-secondary mb-2">Latest Patch Notes</span>
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
          class: "z-50 bg-base-200 flex flex-col overflow-auto",
        }}
      >
        {#snippet title()}
          <div class="flex justify-between items-center border-b border-base-content/20">
            <h3 class="font-bold truncate text-accent">
              {currentPatchNotes.title}
            </h3>
            <button type="button" class="btn btn-circle btn-ghost" onclick={() => (patchNotesDialogOpen = false)}
              >✕</button
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
              onclick={() => currentPatchNotes.url && OpenURL(currentPatchNotes.url)}
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

  <footer
    class="shrink-0 z-10 flex items-center justify-between px-2 text-sm text-base-content bg-base-200 border-t border-base-300"
  >
    <PMTLogo iconHeight={25} textHeight={30} />
    <PageHelp page={$currentPage} />
    <span class="font-mono">v{pkg.version}</span>
  </footer>
</div>
