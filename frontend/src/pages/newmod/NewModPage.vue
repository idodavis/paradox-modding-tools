<!-- Make a new mod. One form picks the game and names the mod, and makes a workspace with the mod in it. -->
<script setup lang="ts">
import { computed, shallowRef } from "vue";
import { useMutation, useQuery } from "@pinia/colada";

import type { DetectedInstall, Game } from "@internal/game/models";
import type { Install } from "@services/app/models";
import * as workspace from "@services/workspace/service";
import { toSentence } from "../../lib/format";
import { gamesQuery, languagesQuery, modFolderQuery } from "../../queries";
import { gameIcon, useSettingsStore } from "../../stores/settings";
import { useWorkspaceStore } from "../../stores/workspace";
import GameInstalls from "../../ui/GameInstalls.vue";
import HowModWorks from "../../ui/HowModWorks.vue";
import StatusFacts from "../../ui/StatusFacts.vue";

const settings = useSettingsStore();
const store = useWorkspaceStore();
const gameId = shallowRef("");
const installId = shallowRef("");
const title = shallowRef("");
const language = shallowRef<string>();
const explaining = shallowRef(false);

const { data: games } = useQuery(gamesQuery);
const { data: languages } = useQuery(() => ({ ...languagesQuery(installId.value), enabled: !!installId.value }));
const { data: parent } = useQuery(() => ({
  ...modFolderQuery({ installId: installId.value, dir: "", name: "" }),
  enabled: !!installId.value,
}));
const { data: modDir, error: nameError } = useQuery(() => ({
  ...modFolderQuery({ installId: installId.value, dir: "", name: title.value.trim() }),
  enabled: !!installId.value && !!title.value.trim(),
}));

const current = computed(() => games.value?.find((game) => game.id === gameId.value));

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
      { name: title.value.trim(), install: installId.value, language: language.value ?? "" },
      [{ title: title.value.trim() }],
      { name: "workspace-ide" },
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

      <form class="flex flex-col gap-5 rounded-lg border border-default bg-elevated p-6" @submit.prevent="make()">
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
          <div class="grid grid-cols-2 gap-5">
            <UFormField label="Mod name" help="Shown in the game's launcher and on the Workshop.">
              <UInput v-model="title" class="w-full" autofocus />
            </UFormField>
            <UFormField label="Language" help="The mod's text starts in this language. Others can be added later.">
              <USelect
                v-model="language"
                :items="languages ?? []"
                value-key="folder"
                placeholder="The game's base language"
                class="w-full"
              />
            </UFormField>
          </div>

          <div class="flex flex-col gap-2">
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
          <UButton
            v-if="settings.isGuided()"
            label="How a mod works"
            icon="i-lucide-layers"
            variant="ghost"
            size="md"
            @click="explaining = true"
          />
          <span class="grow text-end text-12 text-dimmed">
            <template v-if="installId && title.trim()">
              The workspace is named {{ title.trim() }}. All of it can be changed later in Settings.
            </template>
          </span>
          <UButton
            type="submit"
            label="Make the mod"
            color="primary"
            variant="solid"
            size="md"
            :disabled="!installId || !title.trim() || !!nameError"
            :loading="making"
          />
        </div>
      </form>

      <HowModWorks v-model:open="explaining" :game-id="gameId" :mod-title="title" />
    </div>

    <StatusFacts :facts="[{ label: 'Making a mod for', value: current?.name ?? '' }]" />
  </div>
</template>
