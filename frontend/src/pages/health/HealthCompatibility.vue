<!--
  Health's view of the workspace's mods together. It shows a guide to how the game picks a copy, the load order to drag
  for a preview, and the findings about the mods with the open one's detail.
-->
<script setup lang="ts">
import { computed, shallowRef } from "vue";
import { useMutation, useQuery, useQueryCache } from "@pinia/colada";
import { groupBy } from "es-toolkit";
import type { DropdownMenuItem } from "@nuxt/ui";
import { AddIgnoredPair, RemoveIgnoredPair } from "@services/health/service";
import type { Change, Finding } from "@services/health/models";
import type { Mod } from "@services/workspace/models";
import { plural } from "../../lib/format";
import { keys, stateQuery } from "../../queries";
import { useWorkspaceStore } from "../../stores/workspace";
import LoadOrder from "../../ui/LoadOrder.vue";
import OriginChip from "../../ui/OriginChip.vue";
import HealthDetail from "./HealthDetail.vue";
import HealthList from "./HealthList.vue";
import { openSite } from "./HealthSite.vue";

const {
  findings,
  changes,
  mods,
  opened = -1,
  dirty,
  saving,
} = defineProps<{
  /** The findings about the mods together. */
  findings: Finding[];
  /** The names that change hands in the tried order. */
  changes: Change[];
  mods: Mod[];
  /** The open finding's index, or -1 for none. */
  opened?: number;
  /** Whether the tried order differs from the workspace's. */
  dirty: boolean;
  saving: boolean;
}>();
const emit = defineEmits<{ open: [index: number]; save: [] }>();
/** The tried load order, as mod ids. */
const order = defineModel<string[]>("order", { required: true });
const filter = defineModel<string>("filter", { default: "" });

/** A Guide is how one game picks a copy, in the sentences where the games differ. */
type Guide = { hint: string; files: string; kept: string; changes: string };

/** GUIDES holds each game's hint and guide, by the game's id. */
const GUIDES: Readonly<Record<string, Guide>> = {
  ck3: {
    hint: "The load order decides only files that two mods ship at the same path.",
    files:
      "A mod's replace_path makes the game skip its own files in that folder; whether it also skips the folder's subfolders is not documented.",
    kept: "Most things keep the copy read last; events keep the highest override priority, and on a tie the game logs an error and keeps one copy without saying which; a character takes its name, traits, skills, parents and birth from the copy with the highest override priority and combines the rest, and the game calls a tie an error without saying what it keeps; on_actions combine their lists.",
    changes: "Only files at the same path.",
  },
  vic3: {
    hint: "The load order decides only files that two mods ship at the same path.",
    files:
      "A mod's replace_path makes the game skip its own files in that folder. This is documented for CK3 and EU5, whose engine Vic3 shares; whether it also skips the folder's subfolders is not documented.",
    kept: "Most things keep the copy read last; events and GUI types keep the first; where a folder takes INJECT: and REPLACE:, the first plain copy stays and a later one is refused, so mods change it with those keywords, which act after every plain copy whatever their file's name, by kind (INJECT_OR_CREATE first, REPLACE last) and then by file name; character DNA copies all exist.",
    changes: "Only files at the same path.",
  },
  eu5: {
    hint: "The load order decides only files that two mods ship at the same path, and GUI types and templates.",
    files:
      "A mod's files count only inside its in_game, main_menu or loading_screen folder. A mod's replace_path makes the game skip its own files in that folder, not in its subfolders.",
    kept: "Under common/ the first plain copy stays and mods change it with INJECT: and REPLACE:, which act after every plain copy whatever their file's name, by kind (INJECT_OR_CREATE first, REPLACE last) and then by file name; events keep the first; GUI types and templates follow the load order; coats of arms and defines keep the last.",
    changes: "Only files at the same path and GUI types and templates.",
  },
};

/** The checks whose rows have a menu to compare, open or ignore their copies. */
const MENU_CODES = ["contest", "file-contest", "undecided-contest", "shared-message"];

const workspace = useWorkspaceStore();
const cache = useQueryCache();
const { data: state } = useQuery(stateQuery);
const guide = computed((): Guide | undefined => GUIDES[state.value?.game?.id ?? ""]);
const comparing = shallowRef(false);
const open = computed(() => findings[opened]);
/** What each mod would win from the others in the tried order. */
const gains = computed(() => Object.entries(groupBy(changes, (change) => change.after)));

const { mutate: ignorePair } = useMutation({
  mutation: (pair: [string, string]) => AddIgnoredPair(pair[0], pair[1]),
  onSuccess: () => cache.invalidateQueries({ key: keys.health }),
});
const { mutate: stopIgnoring } = useMutation({
  mutation: (pair: [string, string]) => RemoveIgnoredPair(pair[0], pair[1]),
  onSuccess: () => cache.invalidateQueries({ key: keys.health }),
});

/** buildMenu builds the menu of a contest's or a shared key's row, to compare, open or ignore its copies. */
function buildMenu(finding: Finding, index: number): DropdownMenuItem[][] {
  const copies = (finding.sites ?? []).map((site) => ({
    label: `Open ${workspace.originName(site.place.origin)}'s copy in the IDE`,
    icon: "i-lucide-code",
    onSelect: () => openSite(site),
  }));
  const compare = finding.comparable
    ? [
        {
          label: "Compare the two",
          icon: "i-lucide-diff",
          onSelect: () => {
            emit("open", index);
            comparing.value = true;
          },
        },
      ]
    : [];
  const ignores = (finding.pairs ?? []).map(([first, second]) => ({
    label: `Ignore conflicts between ${workspace.originName(first)} and ${workspace.originName(second)}`,
    icon: "i-lucide-eye-off",
    onSelect: () => ignorePair([first, second]),
  }));
  return [[...compare, ...copies], ignores].filter((group) => group.length);
}
</script>

<template>
  <div class="flex h-full min-h-0 min-w-0 flex-col">
    <div class="flex min-h-12 shrink-0 items-center gap-2.5 px-4 pt-1.5">
      <span class="shrink-0 text-16 text-highlighted">Compatibility</span>
      <span class="min-w-0 text-12 text-dimmed">
        {{ dirty ? "Previewing another order." : `${guide?.hint ?? ""} Drag a mod to preview another order.` }}
      </span>
      <span class="grow" />
      <template v-if="dirty">
        <UButton
          label="Back to the workspace order"
          variant="ghost"
          color="neutral"
          class="shrink-0"
          @click="order = mods.map((mod) => mod.id)"
        />
        <UButton label="Save as workspace order" class="shrink-0" :loading="saving" @click="emit('save')" />
      </template>
    </div>
    <UCollapsible v-if="guide" class="mx-4 mb-2.5 shrink-0 rounded-lg border border-default bg-muted text-12">
      <template #default="{ open: guiding }">
        <button type="button" class="flex w-full items-center gap-2 px-3 py-1.5 text-toned hover:text-highlighted">
          <UIcon
            name="i-lucide-chevron-right"
            class="size-3.5 text-dimmed transition-transform"
            :class="guiding && 'rotate-90'"
          />
          <span class="font-medium">How the game picks a copy</span>
        </button>
      </template>
      <template #content>
        <div class="flex max-h-[40vh] flex-col gap-1.5 overflow-y-auto border-t border-default px-3 py-2.5 text-toned">
          <p>
            <b class="font-semibold text-highlighted">Files.</b> The game and every mod are read as one set of folders.
            When two mods ship a file at the same path, only the one lower in the load order is read, and it replaces
            the whole file. {{ guide.files }}
          </p>
          <p>
            <b class="font-semibold text-highlighted">Definitions.</b> Inside a folder, the game reads every file, its
            own and all the mods', in one list sorted by file name (digits before capital letters before small ones; a
            folder's files before its subfolders'). The load order does not change that list, which is why mods name
            files 00_ or zz_.
          </p>
          <p><b class="font-semibold text-highlighted">Which copy is kept.</b> {{ guide.kept }}</p>
          <p>
            <b class="font-semibold text-highlighted">What the load order changes.</b> {{ guide.changes }} Health
            suggests a new order only where it would change the outcome.
          </p>
          <p>
            <b class="font-semibold text-highlighted">Levels.</b> Serious: a mod's change is never used, a dependency is
            missing, or which copy the game keeps is not documented. Warning: a mod's own copy the game ignores, a name
            used from a mod it does not declare, or a shared localization key. Info: a mod overriding the game, or a
            contest between mods where the winner declares the other as a dependency or you ignored the pair. A declared
            dependency lowering a contest is Health's judgment, not a game rule.
          </p>
        </div>
      </template>
    </UCollapsible>
    <div class="flex shrink-0 items-start gap-2 px-4 pb-2.5">
      <span class="me-1.5 shrink-0 text-12 leading-7 font-medium whitespace-nowrap text-muted">Load order</span>
      <LoadOrder v-model:order="order" editable class="min-w-0" />
    </div>
    <div
      v-if="gains.length"
      class="mx-4 mb-2.5 flex shrink-0 flex-col gap-1 rounded-lg border border-default bg-muted px-3 py-2"
    >
      <div v-for="[mod, moved] in gains" :key="mod" class="flex flex-col gap-0.5">
        <span class="text-12 font-medium text-highlighted">
          {{ workspace.originName(mod) }} would then win {{ plural(moved.length, "thing") }}
        </span>
        <span class="max-h-14 overflow-auto text-11 text-dimmed">
          <span v-for="change in moved" :key="`${change.database}:${change.name}`" class="me-3 inline-block">
            <span class="font-mono text-toned">{{ change.name }}</span>
            from {{ workspace.originName(change.before) }}
          </span>
        </span>
      </div>
    </div>
    <HealthList
      v-model:filter="filter"
      :findings="findings"
      empty="No two mods write the same thing, and none uses another's names."
      :opened="opened"
      class="grow"
      @open="emit('open', $event)"
    >
      <template #trailing="{ finding, index }">
        <span class="flex shrink-0 gap-0.75">
          <OriginChip v-for="mod in finding.mods" :key="mod" :origin="mod" dot />
        </span>
        <UButton
          v-if="finding.code === 'ignored-pair'"
          label="Stop ignoring"
          variant="ghost"
          color="neutral"
          size="xs"
          class="shrink-0"
          @click.stop="stopIgnoring([finding.mods?.[0] ?? '', finding.mods?.[1] ?? ''])"
        />
        <UDropdownMenu v-else-if="MENU_CODES.includes(finding.code)" :items="buildMenu(finding, index)">
          <UTooltip text="Compare, open or ignore">
            <UButton
              icon="i-lucide-ellipsis"
              aria-label="Compare, open or ignore"
              variant="ghost"
              color="neutral"
              size="xs"
              square
              class="shrink-0"
              @click.stop
            />
          </UTooltip>
        </UDropdownMenu>
      </template>
      <template #detail>
        <HealthDetail
          v-if="open"
          v-model:comparing="comparing"
          :finding="open"
          :findings="findings"
          :order="order"
          @preview="order = $event"
        />
        <p v-else class="p-4 text-12 text-dimmed">Select a row.</p>
      </template>
    </HealthList>
  </div>
</template>
