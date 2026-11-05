<!-- Health's view of workspace compatibility: a guide, the load order to preview, and the findings. -->
<script setup lang="ts">
import { computed, shallowRef } from "vue";
import { useMutation, useQuery } from "@pinia/colada";
import { groupBy } from "es-toolkit";
import type { DropdownMenuItem } from "@nuxt/ui";
import { AddIgnoredPair, RemoveIgnoredPair } from "@services/health/service";
import type { Change, Finding } from "@services/health/models";
import type { Mod } from "@services/workspace/models";
import { list, listLeftOut, plural } from "../../lib/format";
import { stateQuery } from "../../queries";
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
  opened = undefined,
  dirty,
  saving,
} = defineProps<{
  /** The findings of workspace compatibility. */
  findings: Finding[];
  /** The names that change hands in the tried order. */
  changes: Change[];
  mods: Mod[];
  /** The open finding, or undefined for none. */
  opened?: Finding;
  /** Whether the tried order differs from the workspace's. */
  dirty: boolean;
  saving: boolean;
}>();
const emit = defineEmits<{ open: [finding: Finding]; save: [] }>();
/** The tried load order, as mod ids. */
const order = defineModel<string[]>("order", { required: true });
const filter = defineModel<string>("filter", { default: "" });
const checks = defineModel<string[]>("checks", { default: () => [] });

/** The checks whose rows have a menu to compare, open or ignore their copies. */
const MENU_CODES = ["contest", "file-contest", "undecided-contest", "shared-message"];

const workspace = useWorkspaceStore();
const { data: state } = useQuery(stateQuery);
const guide = computed(() => state.value?.game?.guide);
const comparing = shallowRef(false);
/** What each mod would win from the others in the tried order. */
const gains = computed(() => Object.entries(groupBy(changes, (change) => change.after)));

const { mutate: ignorePair } = useMutation({
  mutation: (pair: [string, string]) => AddIgnoredPair(pair[0], pair[1]),
});
const { mutate: stopIgnoring } = useMutation({
  mutation: (pair: [string, string]) => RemoveIgnoredPair(pair[0], pair[1]),
});
const { mutate: switchMod } = useMutation({
  mutation: (mod: string) => workspace.updateMod(workspace.activeId(), mod, { leftOut: !workspace.isLeftOut(mod) }),
});
/** The mods left out of the checks. */
const leftOut = computed(() => listLeftOut(mods));

/** buildMenu builds the menu of a contest's or a shared key's row, to compare, open or ignore its copies. */
function buildMenu(finding: Finding): DropdownMenuItem[][] {
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
            emit("open", finding);
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
      <span class="shrink-0 text-16 text-highlighted">Workspace compatibility</span>
      <span class="min-w-0 text-12 text-dimmed">
        {{
          dirty
            ? "Previewing another order."
            : `${guide?.hint ?? ""} Drag a mod to try another order, or switch one off to leave it out of these checks.`
        }}
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
            <b class="font-semibold text-highlighted">Levels.</b> Critical: a mod's copy is never used, a file it
            replaces leaves out names still in use, a dependency is missing, or which copy the game keeps is not
            documented. Warning: a localization key several mods define. Info: a mod overriding the game, or a contest
            between mods where the winner declares the other as a dependency or you ignored the pair. A declared
            dependency lowering a contest is Health's judgment, not a game rule.
          </p>
        </div>
      </template>
    </UCollapsible>
    <div class="flex shrink-0 items-start gap-2 px-4 pb-2.5">
      <span class="me-1.5 shrink-0 text-12 leading-7 font-medium whitespace-nowrap text-muted">Load order</span>
      <LoadOrder v-model:order="order" editable class="min-w-0" @toggle="switchMod" />
    </div>
    <p v-if="leftOut.length" class="flex shrink-0 items-center gap-1.5 px-4 pb-2.5 text-12 text-dimmed">
      <UIcon name="i-lucide-eye-off" class="size-3.5 shrink-0" />
      {{ list(leftOut) }}
      {{
        leftOut.length === 1
          ? "is left out: its problems show only in its own view."
          : "are left out: their problems show only in their own views."
      }}
    </p>
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
      v-model:checks="checks"
      :findings="findings"
      empty="No two mods write the same thing, and none uses another's names."
      :opened="opened"
      class="grow"
      @open="emit('open', $event)"
    >
      <template #trailing="{ finding }">
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
        <UDropdownMenu v-else-if="MENU_CODES.includes(finding.code)" :items="buildMenu(finding)">
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
          v-if="opened"
          v-model:comparing="comparing"
          :finding="opened"
          :order="order"
          @preview="order = $event"
        />
        <p v-else class="p-4 text-12 text-dimmed">Select a row.</p>
      </template>
    </HealthList>
  </div>
</template>
