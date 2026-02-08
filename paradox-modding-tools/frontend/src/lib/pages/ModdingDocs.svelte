<script lang="ts">
  import { Tab, Tabs, Card, CardBody } from "@components";
  import { game, gameInstallPathCk3, gameInstallPathEu5 } from "@stores/app";
  import { Scan } from "@services/moddocservice";

  const currentInstallPath = $derived(
    $game === "CK3" ? $gameInstallPathCk3 : $gameInstallPathEu5,
  );

  let filterText = $state("");
  let docFiles = $state<string[]>([]);

  async function scan() {
    try {
      docFiles = await Scan($game, currentInstallPath);
    } catch (error) {
      console.error(error);
    }
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
      contentClass="bg-base-300 border-base-300 p-2"
      ><Card>
        <CardBody>
          <div class="flex flex-wrap items-end gap-4">
            <div class="min-w-200">
              <fieldset class="fieldset mb-4">
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
          </div></CardBody
        >
      </Card>
      <Card>
        <CardBody>
          <div
            class="flex flex-1 min-h-0 overflow-hidden rounded-lg border border-(--p-surface-800)"
          >
            <div
              class="flex-1 min-w-0 min-h-0 flex flex-col overflow-hidden rounded-l pr-1"
            >
              <input
                type="text"
                class="input w-full"
                bind:value={filterText}
                oninput={(e: Event) =>
                  (filterText = (e.target as HTMLInputElement).value)}
                placeholder="Filter by filename"
              />
              <div class="flex-1 min-h-0 overflow-auto">
                <!-- TODO: Finish FileTree component -->
                <!-- <FileTree files={docFiles} /> -->
              </div>
            </div>
            <div
              class="flex-1 min-w-0 min-h-0 flex flex-col overflow-hidden rounded-r -ml-px"
            >
              <!-- TODO: Finish CodeBlock component -->
              <!-- <CodeBlock
                v-if="fileContent !== null"
                :content="fileContent"
                :filename="selectedEntry?.relativePath ?? 'Select a file'"
              /> -->
              <p class="p-4 text-slate-400 bg-dark-input">
                Select a file to view content.
              </p>
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
