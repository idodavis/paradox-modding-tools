<!--
  Make a new mod. With Guidance on, a first screen asks what you want to make; one form then takes the game, the
  folders of the mods the goal works on, and the new mod's name and language, and makes the workspace on the page that
  suits the goal.
-->
<script setup lang="ts">
import { computed, ref, shallowRef } from "vue";
import { useMutation, useQuery } from "@pinia/colada";

import type { DetectedInstall, Game } from "@internal/game/models";
import type { Install } from "@services/app/models";
import * as workspace from "@services/workspace/service";
import { toSentence } from "../../lib/format";
import { gamesQuery, languagesQuery, modCheckQuery, modFolderQuery } from "../../queries";
import { gameIcon, useSettingsStore } from "../../stores/settings";
import { useWorkspaceStore } from "../../stores/workspace";
import FileSelector from "../../ui/FileSelector.vue";
import GameInstalls from "../../ui/GameInstalls.vue";
import HowModWorks from "../../ui/HowModWorks.vue";
import StatusFacts from "../../ui/StatusFacts.vue";

/**
 * GOALS lists what a modder may want to make, in the order the first screen shows them. Each has its texts, how many
 * existing mod folders it works on, whether it makes a new mod, and the page the workspace opens on.
 */
const GOALS = [
  {
    id: "change",
    icon: "i-lucide-pencil",
    label: "Change something in the game",
    blurb:
      "A trait's numbers, a building's cost, a rule. Your mod changes just that entry and leaves the rest to the game.",
    hint: "Find it in the game's files, then Override",
    existing: 0,
    makes: true,
    opens: { name: "workspace-ide" },
  },
  {
    id: "add",
    icon: "i-lucide-plus",
    label: "Add something new",
    blurb: "A trait, an event, a decision. They live in a mod of your own, apart from the game's files.",
    hint: "Name the mod, then New in the explorer",
    existing: 0,
    makes: true,
    opens: { name: "workspace-ide" },
  },
  {
    id: "translate",
    icon: "i-lucide-languages",
    label: "Translate a mod",
    blurb: "Write a mod's text in another language, as a small mod that loads after it.",
    hint: "Pick the mod and the language; Health lists the keys",
    existing: 1,
    makes: true,
    opens: { name: "health", query: { view: "mod" } },
  },
  {
    id: "together",
    icon: "i-lucide-layers",
    label: "Make two mods work together",
    blurb: "A small mod that loads after both and settles what they both change.",
    hint: "Pick the two mods; Health shows what they share",
    existing: 2,
    makes: true,
    opens: { name: "health", query: { view: "compatibility" } },
  },
  {
    id: "fix",
    icon: "i-lucide-activity",
    label: "Fix a mod I have",
    blurb: "See what the game's log and Health say about it, worst first, and work down the list.",
    hint: "Choose its folder; Health opens first",
    existing: 1,
    makes: false,
    opens: { name: "health" },
  },
  {
    id: "browse",
    icon: "i-lucide-book-open",
    label: "Look around the game's files",
    blurb: "A workspace of the game alone, to read how it does things. The files are read only, so nothing can break.",
    hint: "The IDE, with the game's files",
    existing: 0,
    makes: false,
    opens: { name: "workspace-ide" },
  },
] as const;

// the route may name a goal, which starts the form with it
const { goal: routeGoal = "" } = defineProps<{ goal?: string }>();

const settings = useSettingsStore();
const store = useWorkspaceStore();
const goalId = shallowRef<(typeof GOALS)[number]["id"]>(GOALS.find((goal) => goal.id === routeGoal)?.id ?? "add");
const chosen = shallowRef(!!routeGoal);
const gameId = shallowRef("");
const installId = shallowRef("");
// the folders of the existing mods, as many as the goal takes
const folders = ref(["", ""]);
const title = shallowRef("");
const language = shallowRef<string>();
const explaining = shallowRef(false);

const { data: games } = useQuery(gamesQuery);
const { data: languages } = useQuery(() => ({ ...languagesQuery(installId.value), enabled: !!installId.value }));
const { data: parent } = useQuery(() => ({
  ...modFolderQuery({ installId: installId.value, dir: "", name: "" }),
  enabled: !!installId.value,
}));
// the folder the new mod's title makes, or why it cannot be made
const { data: modDir, error: nameError } = useQuery(() => ({
  ...modFolderQuery({ installId: installId.value, dir: "", name: title.value.trim() }),
  enabled: !!installId.value && !!title.value.trim(),
}));
// whether each picked folder can be a mod, as Go checks it
const checks = [0, 1].map((position) =>
  useQuery(() => ({
    ...modCheckQuery({ installId: installId.value, dir: folders.value[position] ?? "" }),
    enabled: !!installId.value && !!folders.value[position] && position < selected.value.existing,
  })),
);

const selected = computed(() => GOALS.find((goal) => goal.id === goalId.value) ?? GOALS[1]);
const current = computed(() => games.value?.find((game) => game.id === gameId.value));
const picked = computed(() => folders.value.slice(0, selected.value.existing));
// the new mod, else the first folder picked, else the game's files
const workspaceName = computed(
  () =>
    (selected.value.makes ? title.value.trim() : "") ||
    picked.value[0]?.split(/[\\/]/).findLast(Boolean) ||
    `${current.value?.name ?? "Game"} files`,
);
const blocked = computed(
  () =>
    !installId.value ||
    picked.value.some((folder) => !folder) ||
    new Set(picked.value).size < picked.value.length ||
    checks.slice(0, selected.value.existing).some((check) => !!check.error.value) ||
    (selected.value.makes && (!title.value.trim() || !!nameError.value)) ||
    (selected.value.id === "translate" && !language.value),
);

// picking a game takes its recorded install, else records the first install found on this computer
const { mutate: pick } = useMutation({
  mutation: async ({ game, recorded, local }: { game: Game; recorded?: Install; local?: DetectedInstall }) => {
    const install =
      recorded ?? (local ? await workspace.AddInstall(game.id, local.path, local.name, local.version) : undefined);
    return { gameId: game.id, installId: install?.id ?? "" };
  },
  onSuccess: (choice) => {
    gameId.value = choice.gameId;
    installId.value = choice.installId;
    language.value = undefined;
  },
});
const { mutate: make, isLoading: making } = useMutation({
  mutation: () =>
    store.create(
      { name: workspaceName.value, install: installId.value, language: language.value ?? "" },
      [
        ...picked.value.map((dir) => ({ dir, title: "" })),
        ...(selected.value.makes ? [{ title: title.value.trim() }] : []),
      ],
      selected.value.opens,
    ),
});
</script>

<template>
  <div class="flex h-full justify-center overflow-y-auto px-6 pt-8 pb-8">
    <div class="flex w-260 max-w-full flex-col gap-5 self-start">
      <div class="flex items-center gap-2.5">
        <UButton
          icon="i-lucide-arrow-left"
          variant="ghost"
          size="sm"
          square
          aria-label="Back to Library"
          :to="{ name: 'library' }"
        />
        <h1 class="text-20">Make a new mod</h1>
        <span class="grow" />
        <UButton label="Cancel" size="md" :to="{ name: 'library' }" />
      </div>

      <div
        v-if="settings.isGuided() && !chosen"
        class="flex flex-col gap-5 rounded-lg border border-default bg-elevated p-6"
      >
        <div class="flex flex-col gap-1">
          <h2 class="text-16">What do you want to make?</h2>
          <p class="text-13 text-dimmed">PMT opens the place that does it. Pick the closest; nothing here is fixed.</p>
        </div>
        <URadioGroup
          v-model="goalId"
          :items="[...GOALS]"
          value-key="id"
          variant="card"
          :ui="{ fieldset: 'grid grid-cols-3 gap-3' }"
        >
          <template #label="{ item }">
            <span class="flex items-center gap-2 text-13 font-semibold">
              <UIcon :name="item.icon" class="size-4 shrink-0 text-dimmed" />{{ item.label }}
            </span>
          </template>
          <template #description="{ item }">
            <span class="flex flex-col gap-2">
              <span class="text-12 text-toned">{{ item.blurb }}</span>
              <span class="flex items-center gap-1.25 text-12 text-dimmed">
                <UIcon name="i-lucide-arrow-right" class="size-3.5 shrink-0" />{{ item.hint }}
              </span>
            </span>
          </template>
        </URadioGroup>
        <div class="flex items-center gap-2 border-t border-default pt-5">
          <UButton label="Set it up myself" variant="ghost" size="md" :to="{ name: 'wizard' }" />
          <span class="grow" />
          <span class="text-11 text-dimmed">Step 1 of 2</span>
          <UButton label="Continue" color="primary" variant="solid" size="md" @click="chosen = true" />
        </div>
      </div>

      <form
        v-else
        class="flex flex-col gap-5 rounded-lg border border-default bg-elevated p-6"
        @submit.prevent="make()"
      >
        <div class="flex flex-col gap-2.5">
          <h2 class="text-16">Which game is it for?</h2>
          <div class="grid grid-cols-3 gap-2.5">
            <GameInstalls v-for="game in games ?? []" v-slot="{ recorded, detected }" :key="game.id" :game-id="game.id">
              <button
                type="button"
                class="flex items-center gap-2.5 rounded-md border bg-muted p-3 text-start disabled:cursor-not-allowed disabled:opacity-60"
                :class="
                  game.id === gameId
                    ? 'border-primary bg-(--accent-soft)'
                    : 'border-default enabled:hover:border-accented'
                "
                :disabled="!recorded.length && !detected.length"
                @click="pick({ game, recorded: recorded[0], local: detected[0] })"
              >
                <img :src="gameIcon(game.id)" :alt="game.name" class="size-7 rounded-sm object-contain" />
                <span class="flex min-w-0 flex-col gap-px">
                  <span class="truncate text-13 font-medium">{{ game.name }}</span>
                  <span class="text-12 text-dimmed">
                    {{
                      recorded[0] || detected[0]
                        ? (recorded[0] ?? detected[0])?.version || "Found on this computer"
                        : "Not found on this computer"
                    }}
                  </span>
                </span>
              </button>
            </GameInstalls>
          </div>
        </div>

        <template v-if="installId">
          <UFormField
            v-for="n in selected.existing"
            :key="n"
            :label="selected.existing > 1 ? `Folder of mod ${n}` : 'Folder of the mod'"
            :help="n === 1 && parent ? `Mods made in the game's launcher are in ${parent}.` : undefined"
            :error="toSentence(checks[n - 1]?.error.value?.message ?? '')"
          >
            <FileSelector v-model:path="folders[n - 1]" folder :filter="[]" />
          </UFormField>

          <div v-if="selected.makes" class="grid grid-cols-2 gap-5">
            <UFormField label="Mod name" help="Shown in the game's launcher and on the Workshop.">
              <UInput v-model="title" class="w-full" autofocus />
            </UFormField>
            <UFormField
              :label="selected.id === 'translate' ? 'Language of the translation' : 'Language'"
              help="The mod's text starts in this language. Others can be added later."
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

          <div v-if="selected.makes" class="flex flex-col gap-2">
            <span class="text-12 font-medium text-toned">PMT makes</span>
            <div class="flex flex-col gap-1 rounded-lg border border-default bg-muted px-3.5 py-2.5">
              <span v-if="nameError" class="text-12 text-error">{{ toSentence(nameError.message) }}</span>
              <span v-else class="truncate font-mono text-12 text-toned">{{ modDir ?? parent }}</span>
              <span class="text-12 text-dimmed"
                >A descriptor, the game's mod folders, readmes and a localization file.</span
              >
            </div>
          </div>
        </template>

        <div class="flex items-center gap-3 border-t border-default pt-5">
          <UButton v-if="settings.isGuided()" label="Back" variant="ghost" size="md" @click="chosen = false" />
          <UButton
            v-if="settings.isGuided()"
            label="How a mod works"
            icon="i-lucide-layers"
            variant="ghost"
            size="md"
            @click="explaining = true"
          />
          <span class="grow text-end text-12 text-dimmed">
            <template v-if="installId">
              The workspace is named {{ workspaceName }}. All of it can be changed later in Settings.
            </template>
          </span>
          <UButton
            type="submit"
            :label="selected.makes ? 'Make the mod' : 'Open the workspace'"
            color="primary"
            variant="solid"
            size="md"
            :disabled="blocked"
            :loading="making"
          />
        </div>
      </form>

      <HowModWorks v-model:open="explaining" :game-id="gameId" :mod-title="title" />
    </div>

    <StatusFacts :facts="[{ label: 'Making a mod for', value: current?.name ?? '' }]" />
  </div>
</template>
