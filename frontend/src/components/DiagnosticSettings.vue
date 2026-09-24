<script setup lang="ts">
/**
 * Each check on or off. The list is the backend's (lsp.Codes), never a copy; a choice is stored as
 * ide.diag.<code> = on|off and read by Diagnose on every call, so it applies on the next keystroke.
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

/** The stored choice, else the default; "" is no choice, as a setting cannot be removed. */
function enabledOf(code: string, fallback: boolean): boolean {
  const stored = settings.values[`ide.diag.${code}`];
  return stored ? stored === "on" : fallback;
}

async function setEnabled(code: string, on: boolean): Promise<void> {
  await settings.set(`ide.diag.${code}`, on ? "on" : "off");
}

/** Every code back to its default: its choice is written "", so a changed default is followed. */
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
      <ul class="divide-y divide-default rounded-md border border-default">
        <li v-for="c in codes" :key="c.code" class="flex items-start gap-4 px-4 py-3">
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
    </template>
  </div>
</template>
