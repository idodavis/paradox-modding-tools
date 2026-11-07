<!-- This wizard creates a workspace from a game install, its mods in load order and a name. -->
<script setup lang="ts">
import { computed, shallowRef, useTemplateRef } from "vue";
import { useMutation, useQuery } from "@pinia/colada";
import { createReusableTemplate } from "@vueuse/core";
import { useSortable } from "@vueuse/integrations/useSortable";
import type { StepperItem } from "@nuxt/ui";

import { gamesQuery, installsQuery, languagesQuery } from "../../queries";
import { gameIcon } from "../../stores/settings";
import { useWorkspaceStore } from "../../stores/workspace";
import FileSelector from "../../ui/FileSelector.vue";
import InstallPicker from "../../ui/InstallPicker.vue";
import SettingsSection from "../../ui/SettingsSection.vue";
import StatusFacts from "../../ui/StatusFacts.vue";
import WorkshopPicker from "../../ui/WorkshopPicker.vue";

/**
 * A WizardMod is a mod folder to add, or a new mod to create when it has no folder, in one of the two lists. Its title
 * is the name shown for it, which a folder picked from disk does not have.
 */
type WizardMod = { id: string; dir: string; title: string; external: boolean };

const STEPS = ["Game", "Install", "Mods", "Details"];
const LIST = "flex flex-col divide-y divide-default rounded-lg border border-default bg-muted";

// A game named by the route starts the wizard at its install.
const { game: routeGame = "" } = defineProps<{ game?: string }>();

const store = useWorkspaceStore();
const step = shallowRef(routeGame ? 1 : 0);
const selectedGame = shallowRef(routeGame);
const selectedInstall = shallowRef("");
const newMod = shallowRef("");
const mods = shallowRef<WizardMod[]>([]);
const externals = shallowRef<WizardMod[]>([]);
const picking = shallowRef(false);
const [DefineRow, Row] = createReusableTemplate<{ mod: WizardMod; index: number }>();
const title = shallowRef("");
const language = shallowRef<string>();

const { data: games } = useQuery(gamesQuery);
const gameId = computed({
  get: () => selectedGame.value || games.value?.[0]?.id || "",
  set: (id: string) => (selectedGame.value = id),
});
const current = computed(() => games.value?.find((game) => game.id === gameId.value));
const { data: installs } = useQuery(() => installsQuery(gameId.value));
const installId = computed({
  get: () =>
    installs.value?.find((install) => install.id === selectedInstall.value)?.id ?? installs.value?.[0]?.id ?? "",
  set: (id: string) => (selectedInstall.value = id),
});
const { data: languages } = useQuery(() => ({ ...languagesQuery(installId.value), enabled: !!installId.value }));

const gameItems = computed(() => (games.value ?? []).map((game) => ({ label: game.name, value: game.id })));
const steps = computed<StepperItem[]>(() =>
  STEPS.map((heading, index) => ({
    title: heading,
    slot: heading.toLowerCase(),
    icon: index < step.value ? "i-lucide-check" : undefined,
    disabled: index > 1 && !installId.value,
  })),
);
/** The folders of the mods in both lists. */
const listed = computed(() => [...mods.value, ...externals.value].map((mod) => mod.dir));
// One entry per step, in order. The mods step asks for nothing.
const blocked = computed(() => [!gameId.value, !installId.value, false, !title.value.trim()][step.value]);

const SORTING = { handle: ".mod-handle", animation: 150, watchElement: true };
useSortable(useTemplateRef<HTMLElement>("list"), mods, SORTING);
useSortable(useTemplateRef<HTMLElement>("externalList"), externals, SORTING);

const { mutate: create, isLoading: creating } = useMutation({
  mutation: () =>
    store.create(
      { name: title.value.trim(), install: installId.value, language: language.value ?? "" },
      [...mods.value, ...externals.value],
      { name: "workspace-ide" },
    ),
});

/** addFolders adds mods to the user's mods or to the external mods, leaving out those listed already. */
function addFolders(folders: Pick<WizardMod, "dir" | "title">[], external: boolean): void {
  const added = folders
    .filter(({ dir }) => !listed.value.includes(dir))
    .map((folder) => ({ ...folder, id: crypto.randomUUID(), external }));
  if (external) externals.value = [...externals.value, ...added];
  else mods.value = [...mods.value, ...added];
}

/** addNewMod adds a mod to create, with the typed title, unless it is blank. */
function addNewMod(): void {
  const text = newMod.value.trim();
  newMod.value = "";
  if (text) mods.value = [...mods.value, { id: crypto.randomUUID(), dir: "", title: text, external: false }];
}

/** removeMod takes a mod out of the load order. */
function removeMod(mod: WizardMod): void {
  mods.value = mods.value.filter((other) => other.id !== mod.id);
  externals.value = externals.value.filter((other) => other.id !== mod.id);
}

/** setStep shows one step of the wizard. It ignores a value that is not a number. */
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
              PMT reads game files from it and scans the game's documentation. You can add more installs later.
            </p>
          </div>
          <InstallPicker v-model="installId" :game-id="gameId" />
        </template>

        <template #mods>
          <div class="flex flex-col gap-1">
            <h2 class="text-16">Which mods do you work on in it?</h2>
            <p class="text-13 text-dimmed">
              Mods load top to bottom after the game; drag them into order. A workspace can start with no mods; you can
              add them later.
            </p>
          </div>
          <DefineRow v-slot="{ mod, index }">
            <div class="flex h-10 items-center gap-2.5 ps-2 pe-1.5">
              <UIcon name="i-lucide-grip-vertical" class="mod-handle size-3.5 cursor-grab text-dimmed" />
              <span class="w-3.5 text-center font-mono text-11 text-dimmed">{{ index + 1 }}</span>
              <span class="min-w-0 grow truncate font-mono text-12">{{ mod.title || mod.dir }}</span>
              <UBadge v-if="!mod.dir" label="New" />
              <UButton
                icon="i-lucide-x"
                variant="ghost"
                size="xs"
                square
                aria-label="Leave out"
                @click="removeMod(mod)"
              />
            </div>
          </DefineRow>
          <div v-if="mods.length" ref="list" :class="LIST">
            <Row v-for="(mod, index) in mods" :key="mod.id" :mod="mod" :index="index" />
          </div>
          <FileSelector
            path=""
            folder
            :filter="[]"
            class="self-start"
            @update:path="addFolders([{ dir: $event, title: '' }], false)"
          >
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

          <div class="flex flex-col gap-1 border-t border-default pt-5">
            <h2 class="text-16">Which mods do you check yours against?</h2>
            <p class="text-13 text-dimmed">
              External mods: someone else's, loaded after yours and never published. Steam's copies of your
              subscriptions are always external.
            </p>
          </div>
          <div v-if="externals.length" ref="externalList" :class="LIST">
            <Row v-for="(mod, index) in externals" :key="mod.id" :mod="mod" :index="mods.length + index" />
          </div>
          <div class="flex gap-2">
            <FileSelector path="" folder :filter="[]" @update:path="addFolders([{ dir: $event, title: '' }], true)">
              <template #default="{ browse }">
                <UButton label="Add an external mod" icon="i-lucide-plus" @click="browse" />
              </template>
            </FileSelector>
            <UButton label="From the Workshop…" icon="i-lucide-download" @click="picking = true" />
          </div>
          <WorkshopPicker
            v-model:open="picking"
            :install-id="installId"
            :taken="listed"
            @add="
              addFolders(
                $event.map((mod) => ({ dir: mod.dir, title: mod.descriptor.name })),
                true,
              )
            "
          />
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
                :items="languages ?? []"
                value-key="folder"
                placeholder="The game's base language"
                class="w-full"
              />
            </UFormField>
          </div>
          <SettingsSection section="appearance" :keys="['appearance.simple']" />
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
