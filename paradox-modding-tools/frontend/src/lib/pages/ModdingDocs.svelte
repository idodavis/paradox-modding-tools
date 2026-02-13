<script lang="ts">
  import { Tab, Tabs, Card, CardBody, FileTree, CodeBlock } from "@components";
  import { game, gameInstallPath, appConstants } from "@stores/app";
  import {
    Scan,
    GetDocPathCache,
    GetDocContent,
  } from "@services/moddocservice";
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

  const wikiUrl = $derived(
    $game === "CK3" ? $appConstants.ck3.wikiUrl : $appConstants.eu5.wikiUrl,
  );
</script>

<div class="p-2">
  <Tabs class="tabs-border tabs-xl">
    <Tab
      tabGroup="modding-docs"
      label="Script docs"
      selected
      contentClass="flex flex-col min-h-0 bg-base-300 border-base-300 h-[calc(92vh-6rem)]"
      ><Card>
        <CardBody>
          <fieldset class="fieldset">
            <legend class="fieldset-legend text-base-content/90"
              >Game install path:</legend
            >
            <div class="flex gap-2">
              <input
                type="text"
                class="input flex-1 min-w-0"
                readonly
                value={$gameInstallPath}
                placeholder="Set in Settings (gear icon in header)"
              />
              <button
                type="button"
                class="btn btn-soft btn-accent"
                disabled={!$gameInstallPath?.trim()}
                onclick={scan}
              >
                Scan
              </button>
            </div>
          </fieldset>
        </CardBody>
      </Card>
      <Card class="flex-1 min-h-0 min-w-0 flex flex-col">
        <CardBody
          class="flex-1 min-h-0 min-w-0 flex flex-col overflow-hidden p-2"
        >
          <div
            class="flex flex-1 min-h-0 overflow-hidden rounded-lg border-2 border-base-content/30"
          >
            <div
              class="flex min-w-0 flex-1 flex-col overflow-hidden bg-base-200 rounded-l-lg border-r-2 border-base-content/20"
            >
              <div
                class="flex h-[5.5rem] flex-col justify-center px-3 py-2 bg-base-300 border-b border-base-content/20"
              >
                <label class="label py-1" for="file-filter-input">
                  <span class="label-text font-semibold text-sm"
                    >Filter Files</span
                  >
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
                <FileTree
                  tree={docTree}
                  filter={filterText}
                  fileColor="text-accent"
                  {onFileClick}
                />
              </div>
            </div>
            <div
              class="flex min-w-0 flex-1 flex-col overflow-hidden bg-dark-input rounded-r-lg shadow-inner"
            >
              <!-- File Content label only (ModdingDocs); CodeBlock header border aligns with file tree -->
              <div
                class="flex h-[calc(2rem-1px)] items-center px-3 bg-base-300 text-sm text-base-content/60"
              >
                File Content
              </div>
              <CodeBlock
                content={selectedEntry?.content ?? ""}
                filename={selectedEntry?.name ?? "Select a file"}
                placeholder="Select a file to view content"
                language="hcl"
              />
            </div>
          </div>
        </CardBody>
      </Card>
    </Tab>
    <Tab
      tabGroup="modding-docs"
      label="Modding Wiki"
      contentClass="bg-base-300 border-base-300 p-2"
    >
      <iframe
        src={wikiUrl}
        title="'Modding Wiki'"
        class="w-full h-[calc(96vh-10rem)]"
      ></iframe>
    </Tab>
  </Tabs>
</div>
