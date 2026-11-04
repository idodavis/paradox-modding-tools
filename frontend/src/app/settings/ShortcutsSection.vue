<!--
  The keyboard shortcuts of every command that has one, a row each. A shortcut changes to the keys pressed after a
  click on it, and a changed one has a button that puts the command's own back.
-->
<script setup lang="ts">
import { useEventListener } from "@vueuse/core";
import { computed, shallowRef } from "vue";

import { SETTING_ROWS } from "../../ui/SettingsSection.vue";
import {
  COMMANDS,
  findBinding,
  joinShortcut,
  listBindings,
  listDefaultBindings,
  listPressedKeys,
  writeBinding,
  type Command,
} from "../commands";

/** MODIFIERS holds the keys that cannot be a shortcut alone. */
const MODIFIERS = new Set(["Control", "Shift", "Alt", "Meta"]);

const { getKbdKey } = useKbd();
// the shortcut taking new keys: its command and its slot among the command's shortcuts
const recording = shallowRef<{ command: Command; slot: number }>();
// the last shortcut that was refused, with why, until the next recording starts
const refused = shallowRef<{ command: Command; slot: number; reason: string }>();

/** One row per shortcut of every command: its keys as set, the command's own, and whether they differ. */
const rows = computed(() =>
  COMMANDS.flatMap((command) => {
    const bindings = listBindings(command);
    return listDefaultBindings(command).map((own, slot) => ({
      command,
      slot,
      kbds: bindings[slot] ?? own,
      own,
      changed: joinShortcut(bindings[slot] ?? own) !== joinShortcut(own),
    }));
  }),
);

useEventListener(window, "keydown", captureKeys, { capture: true });

/** isRecording reports whether the given shortcut of a command is taking new keys. */
function isRecording(command: Command, slot: number): boolean {
  return recording.value?.command === command && recording.value.slot === slot;
}

/** record makes the given shortcut of a command take the next keys pressed, and forgets the last refusal. */
function record(command: Command, slot: number): void {
  recording.value = { command, slot };
  refused.value = undefined;
}

/**
 * captureKeys gives the shortcut taking new keys the ones pressed, before any shortcut runs. Escape stops without a
 * change, a modifier alone waits for its key, and keys another command has are refused under the shortcut.
 */
function captureKeys(event: KeyboardEvent): void {
  const target = recording.value;
  if (!target) return;
  event.preventDefault();
  event.stopImmediatePropagation();
  if (event.key === "Escape") {
    recording.value = undefined;
    return;
  }
  if (MODIFIERS.has(event.key)) return;
  const kbds = listPressedKeys(event);
  const holder = findBinding(kbds);
  recording.value = undefined;
  if (holder && holder !== target.command) {
    const reason = `${kbds.map(getKbdKey).join(" ")} already runs ${holder.label}`;
    refused.value = { command: target.command, slot: target.slot, reason };
    return;
  }
  writeBinding(target.command, target.slot, kbds);
}
</script>

<template>
  <div :class="SETTING_ROWS">
    <div v-for="row in rows" :key="`${row.command.id}:${row.slot}`" class="flex flex-col gap-1">
      <div class="flex items-center gap-2.5 text-13">
        <UIcon v-if="row.command.icon" :name="row.command.icon" class="size-4 shrink-0 text-dimmed" />
        <span v-if="row.command.prefix" class="text-dimmed">{{ row.command.prefix }}</span>
        <span class="min-w-0 grow truncate">{{ row.command.label }}</span>
        <UButton
          variant="outline"
          size="sm"
          class="min-w-36 justify-center"
          :aria-label="`Change the shortcut of ${row.command.label}`"
          @click="record(row.command, row.slot)"
        >
          <span v-if="isRecording(row.command, row.slot)" class="text-12 text-dimmed">Press the keys…</span>
          <span v-else class="flex gap-0.5">
            <UKbd v-for="(key, index) in row.kbds" :key="index" :value="key" />
          </span>
        </UButton>
        <UTooltip text="Back to" :kbds="row.own" :disabled="!row.changed">
          <UButton
            icon="i-lucide-rotate-ccw"
            variant="ghost"
            size="sm"
            square
            :disabled="!row.changed"
            :aria-label="`Put back the shortcut of ${row.command.label}`"
            @click="writeBinding(row.command, row.slot)"
          />
        </UTooltip>
      </div>
      <p v-if="refused?.command === row.command && refused.slot === row.slot" class="text-end text-12 text-warning">
        {{ refused.reason }}
      </p>
    </div>
  </div>
</template>
