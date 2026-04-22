<script lang="ts">
  import { Tab, Tabs, Card, CardBody, FileTree, EditorPane, SplitPane } from "@components";
  import Icon from "@iconify/svelte";
  import { game, gameInstallPath, appConstants } from "@stores/app.svelte";
  import { Scan, GetDocPathCache, GetDocContent } from "@services/moddocservice";
  import { OpenURL } from "@services/browserservice";
  import { BuildTree } from "@services/fileservice";
  import type { TreeNode } from "@services/models";

  let filterText = $state("");
  let docFiles = $state<string[]>([]);
  let docTree = $state<TreeNode[]>([]);
  let selectedEntry = $state<{ name: string; content: string }>();

  $effect(() => {
    const g = $game;
    const path = $gameInstallPath;
    GetDocPathCache(g, path).then(async (cache) => {
      if (cache) {
        docFiles = cache.paths;
        docTree = await BuildTree(docFiles);
      } else {
        docFiles = [];
        docTree = [];
      }
    });
  });

  async function scan() {
    try {
      docFiles = await Scan($game, $gameInstallPath);
      docTree = await BuildTree(docFiles);
    } catch (error) {
      console.error(error);
    }
  }

  async function onFileClick(file: TreeNode) {
    const content = await GetDocContent($game, $gameInstallPath, file.relPath);
    selectedEntry = {
      name: file.name,
      content: content ?? "",
    };
  }

  const wikiUrl = $derived($game === "CK3" ? $appConstants.ck3.wikiUrl : $appConstants.eu5.wikiUrl);
</script>

<div class="relative p-4 max-w-full min-w-0">
  <Tabs class="tabs-border tabs-xl">
    <Tab
      tabGroup="modding-docs"
      label="Script docs"
      selected
      contentClass="flex flex-col min-h-0 bg-base-200/50 border-base-content/10 h-[calc(93.5vh-6rem)]"
      ><Card>
        <CardBody>
          <fieldset class="fieldset">
            <legend class="fieldset-legend text-base-content/90">Game install path:</legend>
            <div class="flex gap-2">
              <input
                type="text"
                class="input flex-1 min-w-0"
                readonly
                value={$gameInstallPath}
                placeholder="Set in Settings (gear icon in header)"
              />
              <button type="button" class="btn btn-soft btn-accent" disabled={!$gameInstallPath?.trim()} onclick={scan}>
                Scan
              </button>
            </div>
          </fieldset>
        </CardBody>
      </Card>
      <Card class="flex-1 min-h-0 min-w-0 flex flex-col">
        <CardBody class="flex-1 min-h-0 min-w-0 flex flex-col overflow-hidden p-2">
          <SplitPane fixedSide="first" class="flex-1 min-h-0">
            {#snippet first()}
              <div class="flex flex-col h-full overflow-hidden bg-base-200">
                <div
                  class="flex h-22 shrink-0 flex-col justify-center px-3 py-2 bg-base-200/50 border-b border-base-content/10"
                >
                  <label class="label py-1" for="file-filter-input">
                    <span class="label-text font-semibold text-sm">Filter Files</span>
                  </label>
                  <div class="relative">
                    <input
                      id="file-filter-input"
                      type="text"
                      class="input input-bordered w-full bg-base-100 focus:bg-base-100"
                      bind:value={filterText}
                      placeholder="Type to filter by filename..."
                    />
                    {#if filterText}
                      <button
                        type="button"
                        class="absolute right-2 top-1/2 -translate-y-1/2 btn btn-ghost btn-xs btn-circle"
                        onclick={() => (filterText = "")}
                        title="Clear filter"
                      >
                        ✕
                      </button>
                    {/if}
                  </div>
                </div>
                <div class="flex-1 min-h-0 overflow-auto p-2">
                  <FileTree tree={docTree} filter={filterText} fileColor="text-accent" {onFileClick} />
                </div>
              </div>
            {/snippet}
            {#snippet second()}
              <div class="flex flex-col h-full overflow-hidden bg-dark-input shadow-inner">
                <div
                  class="flex h-8 shrink-0 items-center px-3 bg-base-200/50 border-b border-base-content/10 text-sm font-bold text-base-content/60"
                >
                  File Content
                </div>
                <EditorPane
                  content={selectedEntry?.content ?? ""}
                  filename={selectedEntry?.name ?? "Select a file"}
                  placeholder="Select a file to view content"
                />
              </div>
            {/snippet}
          </SplitPane>
        </CardBody>
      </Card>
    </Tab>
    <Tab
      tabGroup="modding-docs"
      label="Modding Wiki"
      contentClass="bg-base-200/50 border-base-content/10 p-2 h-[calc(93.5vh-6rem)]"
    >
      <div class="flex flex-col gap-2 h-full">
        <div class="flex justify-end shrink-0">
          <button type="button" class="btn btn-sm btn-secondary btn-outline" onclick={() => OpenURL(wikiUrl)}>
            <Icon icon="mdi:open-in-new" class="size-4" />
            Open in Browser
          </button>
        </div>
        <iframe src={wikiUrl} title="Modding Wiki" class="w-full flex-1 min-h-0"></iframe>
      </div>
    </Tab>
  </Tabs>
</div>
