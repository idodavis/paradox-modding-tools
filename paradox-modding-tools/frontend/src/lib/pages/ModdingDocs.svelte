<script lang="ts">
  import { onMount } from "svelte";
  import { Tab, Tabs, Card, CardBody, FileTree, CodeBlock } from "@components";
  import {
    game,
    gameInstallPathCk3,
    gameInstallPathEu5,
    appConstants,
  } from "@stores/app";
  import { Scan, GetDocPathCache } from "@services/moddocservice";
  import { BuildTree, ReadFileContent } from "@services/fileservice";
  import type { TreeNode } from "@services/models";

  const currentInstallPath = $derived(
    $game === "CK3" ? $gameInstallPathCk3 : $gameInstallPathEu5,
  );
  const currentScriptRootFolder = $derived(
    $game === "CK3"
      ? $appConstants.ck3.scriptRootFolder
      : $appConstants.eu5.scriptRootFolder,
  );

  let filterText = $state("");
  let docFiles = $state<string[]>([]);
  let docTree = $state<TreeNode[]>([]);
  let selectedEntry = $state<{ name: string; content: string }>();

  // Load the cache if it exists
  onMount(async () => {
    const cache = await GetDocPathCache($game, currentInstallPath);
    if (cache) {
      docFiles = cache.paths;
      docTree = await BuildTree(docFiles);
    }
  });

  async function scan() {
    try {
      docFiles = await Scan($game, currentInstallPath);
      docTree = await BuildTree(docFiles);
    } catch (error) {
      console.error(error);
    }
  }

  async function onFileClick(file: TreeNode) {
    selectedEntry = {
      name: file.name,
      content: await ReadFileContent(
        currentInstallPath + "/" + currentScriptRootFolder + "/" + file.relPath,
      ),
    };
  }

  const wikiUrl = $derived(
    $game === "EU5"
      ? "https://eu5.paradoxwikis.com/Modding"
      : "https://ck3.paradoxwikis.com/Modding",
  );
</script>

<div class="p-2">
  <Tabs class="tabs-border tabs-xl">
    <Tab
      tabGroup="modding-docs"
      label="Script docs"
      selected
      contentClass="flex flex-col bg-base-300 border-base-300"
      ><Card>
        <CardBody>
          <div class="flex flex-wrap items-end gap-4">
            <div class="min-w-200">
              <fieldset class="fieldset">
                <legend class="fieldset-legend text-base-content/90"
                  >Game install path:</legend
                >
                <input
                  type="text"
                  class="input w-full max-w-2xl"
                  readonly
                  value={$game === "CK3"
                    ? $gameInstallPathCk3
                    : $gameInstallPathEu5}
                  placeholder="Set in Settings (gear icon in header)"
                />
                <button
                  type="button"
                  class="btn btn-soft btn-accent btn-wide"
                  onclick={scan}
                >
                  Scan
                </button>
              </fieldset>
            </div>
          </div>
        </CardBody>
      </Card>
      <Card class="flex-1 min-h-0 flex flex-col">
        <CardBody>
          <div class="flex flex-1 rounded-lg border border-base-content/20">
            <div class="flex-1 flex flex-col overflow-hidden bg-base-200">
              <input
                type="text"
                class="input w-full"
                bind:value={filterText}
                oninput={(e: Event) =>
                  (filterText = (e.target as HTMLInputElement).value)}
                placeholder="Filter by filename"
              />
              <FileTree
                tree={docTree}
                filter={filterText}
                fileColor="text-accent"
                {onFileClick}
              />
            </div>
            <div class="flex-1 bg-dark-input">
              <CodeBlock
                class="min-h-200"
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
