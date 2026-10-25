<!-- The new workspace wizard: the game, the install, the mods and their load order, and the name, one step each. -->
<script setup lang="ts">
import { computed, shallowRef, useTemplateRef } from "vue";
import { useRouter } from "vue-router";
import { useMutation, useQuery } from "@pinia/colada";
import { useSortable } from "@vueuse/integrations/useSortable";
import type { StepperItem } from "@nuxt/ui";

import { describeLanguages } from "../../lib/format";
import { gamesQuery, installsQuery, languagesQuery } from "../../queries";
import { gameIcon } from "../../stores/settings";
import { useWorkspaceStore } from "../../stores/workspace";
import FileSelector from "../../ui/FileSelector.vue";
import InstallPicker from "../../ui/InstallPicker.vue";
import StatusFacts from "../../ui/StatusFacts.vue";
import * as workspace from "@services/workspace/service";

const STEPS = ["Game", "Install", "Mods", "Details"];

// "New workspace for this game" names the game, so the wizard starts at its install
const { game: routeGame = "" } = defineProps<{ game?: string }>();

const router = useRouter();
const store = useWorkspaceStore();
const step = shallowRef(routeGame ? 1 : 0);
const selectedGame = shallowRef(routeGame);
const selectedInstall = shallowRef("");
const newMod = shallowRef("");
// a mod is an existing folder, or a title for a mod made once the workspace exists
const mods = shallowRef<{ id: string; dir: string; title: string }[]>([]);
const title = shallowRef("");
const language = shallowRef<string>();

const { data: games } = useQuery(gamesQuery);
const gameId = computed({
  get: () => selectedGame.value || games.value?.[0]?.id || "",
  set: (id: string) => (selectedGame.value = id),
});
const current = computed(() => games.value?.find((one) => one?.id === gameId.value));
const { data: allInstalls } = useQuery(installsQuery);
const installs = computed(() => allInstalls.value?.filter((install) => install.game === gameId.value) ?? []);
const installId = computed({
  get: () => installs.value.find((install) => install.id === selectedInstall.value)?.id ?? installs.value[0]?.id ?? "",
  set: (id: string) => (selectedInstall.value = id),
});
const { data: languages } = useQuery(() => ({ ...languagesQuery(installId.value), enabled: !!installId.value }));

const gameItems = computed(() =>
  (games.value ?? []).filter((one) => one !== null).map((one) => ({ label: one.name, value: one.id })),
);
const steps = computed<StepperItem[]>(() =>
  STEPS.map((heading, index) => ({
    title: heading,
    slot: heading.toLowerCase(),
    icon: index < step.value ? "i-lucide-check" : undefined,
    disabled: index > 1 && !installId.value,
  })),
);
// a step goes on once what it asks for is set; the mods step asks for nothing
const blocked = computed(() => [!gameId.value, !installId.value, false, !title.value.trim()][step.value]);

useSortable(useTemplateRef<HTMLElement>("list"), mods, { handle: ".mod-handle", animation: 150, watchElement: true });

const { mutate: create, isLoading: creating } = useMutation({
  mutation: async () => {
    const record = await workspace.CreateWorkspace({
      id: "",
      name: title.value.trim(),
      install: installId.value,
      mods: [],
      language: language.value ?? "",
      homePage: "",
      gameColor: "",
      resetTabs: false,
      showBinaries: false,
      settings: null,
    });
    // in load order: each mod is added after the one before it
    for (const mod of mods.value) {
      if (mod.dir) await workspace.AddMod(record.id, mod.dir);
      else await workspace.CreateMod(record.id, "", mod.title, language.value ?? "");
    }
    return record;
  },
  onSuccess: (record) => {
    store.open(record.id);
    void router.replace({ name: "workspace-ide", params: { id: record.id } });
  },
});

/** addFolder adds an existing mod folder to the load order. A folder that is in it already adds nothing. */
function addFolder(dir: string): void {
  if (!mods.value.some((mod) => mod.dir === dir))
    mods.value = [...mods.value, { id: crypto.randomUUID(), dir, title: "" }];
}

/** addNewMod adds a mod to make, with the typed title. A blank title adds nothing. */
function addNewMod(): void {
  const text = newMod.value.trim();
  newMod.value = "";
  if (text) mods.value = [...mods.value, { id: crypto.randomUUID(), dir: "", title: text }];
}

/** setStep shows the step with the given number. It ignores a value that is not a number. */
function setStep(index: string | number | undefined): void {
  if (typeof index === "number") step.value = index;
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

      <UStepper
        :model-value="step"
        :items="steps"
        :ui="{ content: 'flex flex-col gap-5 rounded-lg border border-default bg-elevated p-6' }"
        @update:model-value="setStep"
      >
        <template #game>
          <div class="flex flex-col gap-1">
            <h2 class="text-16">Which game is this workspace for?</h2>
            <p class="text-13 text-dimmed">PMT reads that game's files and its scripting language.</p>
          </div>
          <URadioGroup v-model="gameId" :items="gameItems" variant="card">
            <template #label="{ item }">
              <span class="flex items-center gap-2">
                <img :src="gameIcon(`${item.value}`)" :alt="item.label" class="size-4.5 rounded-sm" />
                {{ item.label }}
              </span>
            </template>
          </URadioGroup>
        </template>

        <template #install>
          <div class="flex flex-col gap-1">
            <h2 class="text-16">Which {{ current?.name }} install should this workspace use?</h2>
            <p class="text-13 text-dimmed">
              PMT reads game files from it and scans its script_docs. You can add more installs later.
            </p>
          </div>
          <InstallPicker v-model="installId" :game-id="gameId" />
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
            <div v-for="(mod, index) in mods" :key="mod.id" class="flex h-10 items-center gap-2.5 ps-2 pe-1.5">
              <UIcon name="i-lucide-grip-vertical" class="mod-handle size-3.5 cursor-grab text-dimmed" />
              <span class="w-3.5 text-center font-mono text-11 text-dimmed">{{ index + 1 }}</span>
              <span class="min-w-0 grow truncate font-mono text-12">{{ mod.dir || mod.title }}</span>
              <UBadge v-if="!mod.dir" label="New" />
              <UButton
                icon="i-lucide-x"
                variant="ghost"
                size="xs"
                square
                aria-label="Leave out"
                @click="mods = mods.filter((one) => one !== mod)"
              />
            </div>
          </div>
          <FileSelector path="" folder :filter="[]" class="self-start" @update:path="addFolder">
            <template #default="{ browse }">
              <UButton label="Add an existing mod" icon="i-lucide-plus" @click="browse" />
            </template>
          </FileSelector>
          <form class="flex items-end gap-2" @submit.prevent="addNewMod">
            <UFormField label="Create a new mod" class="grow">
              <UInput v-model="newMod" placeholder="Its name; it is made with the workspace" class="w-full" />
            </UFormField>
            <UButton type="submit" label="Add" icon="i-lucide-plus" :disabled="!newMod.trim()" />
          </form>
        </template>

        <template #details>
          <div class="flex flex-col gap-1">
            <h2 class="text-16">Name the workspace</h2>
            <p class="text-13 text-dimmed">Shown in the Library and the workspace switcher.</p>
          </div>
          <div class="grid grid-cols-2 gap-6">
            <UFormField label="Name" required>
              <UInput v-model="title" :placeholder="`My ${current?.shortName ?? ''} mod`" class="w-full" />
            </UFormField>
            <UFormField
              label="Localization language"
              help="Used for localization gaps, hover text and new mods' files."
            >
              <USelect
                v-model="language"
                :items="describeLanguages(languages)"
                placeholder="The game's base language"
                class="w-full"
              />
            </UFormField>
          </div>
        </template>
      </UStepper>

      <div class="flex items-center gap-2 border-t border-default pt-5">
        <UButton v-if="step > 0" label="Back" variant="ghost" size="md" @click="step--" />
        <span class="grow" />
        <span class="text-11 text-dimmed">Step {{ step + 1 }} of {{ STEPS.length }}</span>
        <UButton
          :label="step < STEPS.length - 1 ? 'Continue' : 'Create workspace'"
          color="primary"
          variant="solid"
          size="md"
          :disabled="blocked"
          :loading="creating"
          @click="step < STEPS.length - 1 ? step++ : create()"
        />
      </div>
    </div>

    <StatusFacts :facts="[{ label: 'Creating a workspace for', value: current?.name ?? '' }]" />
  </div>
</template>
