<script setup lang="ts">
/**
 * A new workspace: the game, its install, the mods in load order, then a name. It
 * opens in the IDE once made.
 */
import { computed, shallowRef, useTemplateRef } from "vue";
import { useRoute, useRouter } from "vue-router";
import { useMutation, useQuery } from "@pinia/colada";
import { createReusableTemplate } from "@vueuse/core";
import { useSortable } from "@vueuse/integrations/useSortable";
import type { StepperItem } from "@nuxt/ui";
import { AddWorkspaceMod, CreateWorkspace, SetWorkspaceLocLang } from "@services/workspaceservice";
import CreateModForm from "../../ui/CreateModForm.vue";
import { pickDirectory } from "../../lib/nativeDialog";
import { installsQuery } from "../../queries";
import { useWorkspaceStore } from "../../stores/workspace";
import { LOC_LANGUAGES } from "../../app/pages";
import InstallPicker from "../../ui/InstallPicker.vue";
import StatusFacts from "../../ui/StatusFacts.vue";

const route = useRoute();
const router = useRouter();
const store = useWorkspaceStore();
const createModal = useOverlay().create(CreateModForm);
const [DefineFooter, Footer] = createReusableTemplate<{ next: string; disabled: boolean }>();

// "New workspace for this game" names the game, so the wizard starts at its install
const asked = typeof route.query.game === "string" ? route.query.game : "";
const gameId = shallowRef(asked || store.activeWorkspace?.gameId || "ck3");
const step = shallowRef(asked ? 1 : 0);
const chosenInstall = shallowRef("");
const mods = shallowRef<string[]>([]);
const name = shallowRef("");
const locLang = shallowRef("english");

const game = computed(() => store.game(gameId.value));
const { data: installs } = useQuery(() => installsQuery(gameId.value));
// the install picked for this game, else its first
const installId = computed({
  get: () => installs.value?.find((install) => install.id === chosenInstall.value)?.id ?? installs.value?.[0]?.id ?? "",
  set: (id: string) => (chosenInstall.value = id),
});

const steps = computed<StepperItem[]>(() =>
  ["Game", "Install", "Mods", "Details"].map((title, i) => ({
    title,
    slot: title.toLowerCase(),
    icon: i < step.value ? "i-lucide-check" : undefined,
    disabled: i > 1 && !installId.value,
  })),
);
const gameItems = computed(() => store.games.map((g) => ({ label: g.name, value: g.id })));

const list = useTemplateRef("list");
useSortable(list, mods, { handle: ".mod-handle", animation: 150, watchElement: true });

async function addExisting(): Promise<void> {
  const path = await pickDirectory("A mod folder");
  if (path && !mods.value.includes(path)) mods.value = [...mods.value, path];
}

async function createNew(): Promise<void> {
  const supportedVersion = installs.value?.find((install) => install.id === installId.value)?.versionDetected ?? "";
  const path = await createModal.open({ gameId: gameId.value, locLang: locLang.value, supportedVersion });
  if (path) mods.value = [...mods.value, path];
}

const { mutate: create, isLoading: creating } = useMutation({
  mutation: async () => {
    const ws = await CreateWorkspace(gameId.value, name.value.trim(), installId.value);
    if (!ws) throw new Error("The workspace was not made");
    await SetWorkspaceLocLang(ws.id, locLang.value);
    // in load order; Go names each from its descriptor
    for (const path of mods.value) await AddWorkspaceMod(ws.id, path);
    return ws;
  },
  onSuccess: (ws) => void router.replace({ name: "workspace-ide", params: { id: ws.id } }),
});

function goTo(at: string | number | undefined): void {
  if (typeof at === "number") step.value = at;
}

function next(): void {
  if (step.value < 3) step.value++;
  else create();
}
</script>

<template>
  <div class="flex h-full justify-center overflow-y-auto px-6 pt-10 pb-8">
    <div class="flex w-215 max-w-full flex-col gap-6">
      <div class="flex items-center gap-2.5">
        <UButton
          icon="i-lucide-arrow-left"
          variant="ghost"
          size="sm"
          square
          aria-label="Back to Library"
          :to="{ name: 'library' }"
        />
        <h1 class="text-20">New workspace</h1>
        <span class="grow" />
        <UButton label="Cancel" size="md" :to="{ name: 'library' }" />
      </div>

      <DefineFooter v-slot="{ next: label, disabled }">
        <div class="flex items-center gap-2 border-t border-default pt-5">
          <UButton v-if="step > 0" label="Back" variant="ghost" size="md" @click="step--" />
          <span class="grow" />
          <span class="text-11 text-dimmed">Step {{ step + 1 }} of 4</span>
          <UButton
            :label="label"
            color="primary"
            variant="solid"
            size="md"
            :disabled="disabled"
            :loading="creating"
            @click="next"
          />
        </div>
      </DefineFooter>

      <UStepper
        :model-value="step"
        :items="steps"
        :ui="{ content: 'flex flex-col gap-5 rounded-lg border border-default bg-elevated p-6' }"
        @update:model-value="goTo"
      >
        <template #game>
          <div class="flex flex-col gap-1">
            <h2 class="text-16">Which game is this workspace for?</h2>
            <p class="text-13 text-dimmed">PMT reads that game's files and its scripting language.</p>
          </div>
          <URadioGroup v-model="gameId" :items="gameItems" variant="card" />
          <Footer next="Continue" :disabled="!gameId" />
        </template>

        <template #install>
          <div class="flex flex-col gap-1">
            <h2 class="text-16">Which {{ game?.name }} install should this workspace use?</h2>
            <p class="text-13 text-dimmed">
              PMT reads game files from it and scans its script_docs. You can add more installs later in Settings.
            </p>
          </div>
          <InstallPicker v-model="installId" :game-id="gameId" />
          <Footer next="Continue" :disabled="!installId" />
        </template>

        <template #mods>
          <div class="flex flex-col gap-1">
            <h2 class="text-16">Which mods do you work on in it?</h2>
            <p class="text-13 text-dimmed">
              Mods load top to bottom after the game; drag them into order. You can also leave the game files alone.
            </p>
          </div>
          <div
            v-if="mods.length"
            ref="list"
            class="flex flex-col divide-y divide-default rounded-lg border border-default bg-muted"
          >
            <div v-for="(path, i) in mods" :key="path" class="flex h-10 items-center gap-2.5 ps-2 pe-1.5">
              <UIcon name="i-lucide-grip-vertical" class="mod-handle size-3.5 cursor-grab text-dimmed" />
              <span class="w-3.5 text-center font-mono text-11 text-dimmed">{{ i + 1 }}</span>
              <span class="min-w-0 grow truncate font-mono text-12">{{ path }}</span>
              <UButton
                icon="i-lucide-x"
                variant="ghost"
                size="xs"
                square
                aria-label="Leave out"
                @click="mods = mods.filter((p) => p !== path)"
              />
            </div>
          </div>
          <div class="flex gap-2">
            <UButton label="Add an existing mod" icon="i-lucide-plus" @click="addExisting" />
            <UButton label="Create a new mod" @click="createNew" />
          </div>
          <Footer next="Continue" :disabled="false" />
        </template>

        <template #details>
          <div class="flex flex-col gap-1">
            <h2 class="text-16">Name the workspace</h2>
            <p class="text-13 text-dimmed">Shown in the Library and the workspace switcher.</p>
          </div>
          <div class="grid grid-cols-2 gap-6">
            <UFormField label="Name" required>
              <UInput v-model="name" :placeholder="`My ${game?.shortName ?? ''} mod`" class="w-full" />
            </UFormField>
            <UFormField
              label="Localization language"
              help="Used for localization gaps, hover text and new mods' files."
            >
              <USelect v-model="locLang" :items="LOC_LANGUAGES" class="w-full" />
            </UFormField>
          </div>
          <Footer next="Create workspace" :disabled="!name.trim() || !installId" />
        </template>
      </UStepper>
    </div>

    <StatusFacts>
      <span>Creating a workspace for {{ game?.name }}</span>
    </StatusFacts>
  </div>
</template>
