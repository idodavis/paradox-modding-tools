<script setup lang="ts">
/**
 * Per-diagnostic on/off control.
 *
 * The registry is the backend's (lsp/codes.go), never a copy: a code listed here
 * but not there is a toggle that does nothing, and a code there but not here is
 * a check nobody can turn off. Choices persist under `ide.diag.<code>` as "on" or
 * "off", which IdeService.Diagnose reads on every call — so a change takes
 * effect on the next keystroke with no rescan.
 */
import { computed } from "vue";
import { useQuery } from "@pinia/colada";
import { DiagnosticCodes } from "@services/ideservice";
import { useSettingsStore } from "../stores/settings";

defineOptions({ name: "DiagnosticSettings" });

const settings = useSettingsStore();

const { data, isLoading, error } = useQuery({
  key: ["diagnostic-codes"],
  query: () => DiagnosticCodes(),
});

const codes = computed(() => data.value ?? []);

/** Groups in display order. The backend owns which group a code is in; this
 * only names them, so a new group shows up unlabelled rather than vanishing. */
const GROUPS = [
  {
    id: "check",
    label: "Checks",
    blurb:
      "Judged against what the game declares. Turning one off hides its findings everywhere, without a rescan.",
  },
  {
    id: "parse",
    label: "Parse errors",
    blurb:
      "The file does not read as valid script or localization. The game cannot load it either, so these are worth leaving on.",
  },
];

const groups = computed(() =>
  GROUPS.map((g) => ({ ...g, rows: codes.value.filter((c) => c.group === g.id) }))
    .filter((g) => g.rows.length > 0),
);

const ungrouped = computed(() =>
  codes.value.filter((c) => !GROUPS.some((g) => g.id === c.group)),
);

/** The stored on/off choice, or the code's own default when the user has not
 * chosen. An empty stored value counts as "not chosen": SaveSettings can only
 * write keys, never remove them, so clearing a choice means writing "". */
function enabledOf(code: string, fallback: boolean): boolean {
  const stored = settings.values[`ide.diag.${code}`];
  return stored ? stored === "on" : fallback;
}

async function setEnabled(code: string, on: boolean): Promise<void> {
  await settings.set(`ide.diag.${code}`, on ? "on" : "off");
}

/** Restore every code to its default.
 *
 * Writes "" rather than the default value, and rather than deleting the key:
 * deleting does nothing (SaveSettings merges, so a removed key keeps its old
 * stored value and the UI silently disagrees with the engine), while writing
 * the default in would pin it -- a code that later graduates from off to on
 * should follow, and a user who once pressed this should not be stuck on the
 * old default forever. EnabledCodes resolves "" as unset. */
async function resetAll(): Promise<void> {
  const next = { ...settings.values };
  let touched = false;
  for (const c of codes.value) {
    const key = `ide.diag.${c.code}`;
    if (!next[key]) continue;
    next[key] = "";
    touched = true;
  }
  if (!touched) return;
  settings.values = next;
  await settings.save();
}

const changed = computed(() =>
  codes.value.some((c) => !!settings.values[`ide.diag.${c.code}`]),
);
</script>

<template>
  <div class="space-y-4">
    <div class="flex items-start justify-between gap-4">
      <p class="text-sm text-muted">
        Severity for each check. Changes apply on the next keystroke — no rescan.
      </p>
      <UButton
        label="Reset to defaults"
        color="neutral"
        variant="outline"
        size="xs"
        :disabled="!changed || settings.saving"
        @click="resetAll()"
      />
    </div>

    <p v-if="error" class="text-sm text-error">
      Could not load the diagnostic registry.
    </p>
    <p v-else-if="isLoading" class="text-sm text-muted">Loading checks…</p>

    <template v-else>
      <section v-for="g in [...groups, ...(ungrouped.length ? [{ id: 'other', label: 'Other', blurb: '', rows: ungrouped }] : [])]" :key="g.id" class="space-y-2">
        <div>
          <h4 class="text-sm font-medium text-highlighted">{{ g.label }}</h4>
          <p v-if="g.blurb" class="text-xs text-muted">{{ g.blurb }}</p>
        </div>
        <ul class="divide-y divide-default rounded-md border border-default">
          <li v-for="c in g.rows" :key="c.code" class="flex items-start gap-4 px-4 py-3">
            <div class="min-w-0 flex-1">
              <code class="text-sm font-medium">{{ c.code }}</code>
              <p class="mt-0.5 text-sm text-muted">{{ c.summary }}</p>
            </div>
            <USwitch
              :model-value="enabledOf(c.code, c.default)"
              size="sm"
              class="shrink-0"
              @update:model-value="setEnabled(c.code, $event)"
            />
          </li>
        </ul>
      </section>
    </template>
  </div>
</template>
