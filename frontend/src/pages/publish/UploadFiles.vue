<!-- A dialog of the files an upload sends, the rules that leave files out with the files each leaves out, and the mod's
own patterns. -->
<script setup lang="ts">
import { computed, shallowRef } from "vue";
import { useMutation, useQuery } from "@pinia/colada";
import { groupBy } from "es-toolkit";

import { bytes, describeCount } from "../../lib/format";
import { ignoreQuery, rulesQuery, uploadQuery } from "../../queries";
import { useSettingsStore } from "../../stores/settings";
import { useWorkspaceStore } from "../../stores/workspace";
import * as steam from "@services/steam/service";
import type { Mod as ModRecord } from "@services/app/models";
import type { Mod } from "@services/workspace/models";

const { mod, listingKey } = defineProps<{ mod: Mod; listingKey: string }>();
const open = defineModel<boolean>("open", { required: true });

const workspace = useWorkspaceStore();
const settings = useSettingsStore();
const toast = useToast();
const tab = shallowRef("included");
// The latest write of the mod's record, which an export waits for.
let lastUpdate: Promise<void> | undefined;

const enabled = computed(() => open.value && workspace.isOpen());
const { data: rules } = useQuery(rulesQuery);
const { data: upload } = useQuery(() => ({
  ...uploadQuery({ modId: mod.id, key: listingKey }),
  enabled: enabled.value,
}));
// the text of the mod's .workshop-ignore file, or "" when the folder has none
const { data: ignore, refetch: refetchIgnore } = useQuery(() => ({ ...ignoreQuery(mod.id), enabled: enabled.value }));

const included = computed(() => upload.value?.files?.map((file) => file.rel) ?? []);
const excluded = computed(() => Object.keys(upload.value?.excluded ?? {}).sort());
/** Every rule with the files it leaves out, then each other reason a file is left out for, with its files. */
const exclusions = computed(() => {
  const byReason = groupBy(excluded.value, (rel) => upload.value?.excluded?.[rel] ?? "");
  const ruleRows = (rules.value ?? []).map((rule) => ({ ...rule, files: byReason[rule.reason] ?? [] }));
  const others = Object.entries(byReason)
    .filter(([reason]) => !ruleRows.some((rule) => rule.reason === reason))
    .map(([reason, files]) => ({ pattern: "", reason, files }));
  return [...ruleRows, ...others];
});
const tabs = computed(() => [
  { label: `Sent (${describeCount(included.value.length)})`, value: "included", icon: "i-lucide-upload" },
  { label: `Left out (${describeCount(excluded.value.length)})`, value: "excluded", icon: "i-lucide-eye-off" },
]);

const { mutate: update } = useMutation({
  mutation: (changes: Partial<ModRecord>) => (lastUpdate = workspace.updateMod(workspace.activeId(), mod.id, changes)),
});
const { mutate: exportIgnore } = useMutation({
  mutation: async () => {
    await lastUpdate?.catch(() => undefined);
    await steam.WriteIgnore(mod.id);
  },
  onSuccess: () => {
    toast.add({ title: "Wrote the patterns to .workshop-ignore in the mod's folder.", color: "success" });
    void refetchIgnore();
  },
});
</script>

<template>
  <UModal
    v-model:open="open"
    title="What an upload sends"
    description="The files of the next upload, the files it leaves out, and the rule that leaves each one out."
    :ui="{
      content: 'h-[min(48rem,90vh)] sm:max-w-[min(70rem,92vw)]',
      body: 'flex min-h-0 grow gap-6 overflow-hidden',
    }"
  >
    <template #body>
      <div class="flex min-h-0 min-w-0 grow basis-0 flex-col gap-2">
        <UTabs
          :model-value="tab"
          :items="tabs"
          :content="false"
          size="sm"
          class="shrink-0"
          @update:model-value="tab = `${$event}`"
        />
        <p v-if="tab === 'included' && upload?.bytes" class="shrink-0 text-12 text-dimmed">
          {{ bytes(upload.bytes) }} in all
        </p>
        <UScrollArea
          v-if="tab === 'included'"
          v-slot="{ item }"
          :items="included"
          :virtualize="{ estimateSize: settings.toPixels(1.75), skipMeasurement: true }"
          class="min-h-0 grow rounded-lg border border-default bg-muted"
        >
          <div class="flex h-7 items-center px-3 font-mono text-12 text-toned">
            <span class="min-w-0 truncate" :title="item">{{ item }}</span>
          </div>
        </UScrollArea>
        <div v-else class="flex min-h-0 grow flex-col gap-2 overflow-y-auto">
          <section
            v-for="row in exclusions"
            :key="`${row.pattern}:${row.reason}`"
            class="flex flex-col gap-1.5 rounded-lg border border-default bg-muted px-3 py-2.5 text-12"
          >
            <p class="text-dimmed">
              <code v-if="row.pattern" class="me-1.5 rounded-sm bg-elevated px-1 py-px font-mono text-default">
                {{ row.pattern }}
              </code>
              {{ row.reason }}
            </p>
            <p v-for="rel in row.files" :key="rel" class="truncate font-mono text-toned" :title="rel">{{ rel }}</p>
            <p v-if="!row.files.length" class="text-11 text-dimmed">Nothing in this mod.</p>
          </section>
        </div>
      </div>

      <div class="flex min-h-0 w-88 shrink-0 flex-col gap-5 overflow-y-auto">
        <section class="flex flex-col gap-2">
          <span class="text-12 font-medium text-toned">The mod's own patterns</span>
          <UTextarea
            :model-value="mod.ignore"
            :model-modifiers="{ lazy: true }"
            :rows="6"
            placeholder="Gitignore patterns, one per line, such as gfx/source/**"
            class="w-full font-mono"
            @update:model-value="update({ ignore: `${$event}` })"
          />
          <div class="flex gap-1">
            <UTooltip text="The mod's folder has no .workshop-ignore file" :disabled="!!ignore">
              <span>
                <UButton
                  label="Import .workshop-ignore"
                  icon="i-lucide-file-down"
                  variant="ghost"
                  size="sm"
                  :disabled="!ignore"
                  @click="update({ ignore })"
                />
              </span>
            </UTooltip>
            <UButton
              label="Export .workshop-ignore"
              icon="i-lucide-file-up"
              variant="ghost"
              size="sm"
              @click="exportIgnore()"
            />
          </div>
        </section>

        <p class="flex items-start gap-2 rounded-lg bg-muted p-3 text-12 text-dimmed">
          <UIcon name="i-lucide-info" class="mt-0.5 size-3.5 shrink-0" />
          <span>
            PMT keeps the mod's Workshop page in <span class="font-mono text-toned">.workshop/</span> inside the mod. An
            upload never sends it.
          </span>
        </p>
      </div>
    </template>
  </UModal>
</template>
