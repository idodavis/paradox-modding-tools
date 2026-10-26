<!--
  This modal shows the files a mod's upload leaves out, and edits the mod's own patterns for them.
-->
<script setup lang="ts">
import { computed, shallowRef } from "vue";
import { useMutation, useQuery } from "@pinia/colada";

import { describeCount } from "../../lib/format";
import { rulesQuery, uploadQuery } from "../../queries";
import { useWorkspaceStore } from "../../stores/workspace";
import * as steam from "@services/steam/service";
import type { Mod as ModRecord } from "@services/app/models";
import type { Mod } from "@services/workspace/models";

const { mod } = defineProps<{ mod: Mod }>();
const open = defineModel<boolean>("open", { required: true });

const store = useWorkspaceStore();
const toast = useToast();
const tab = shallowRef("included");
// The latest write of the mod's record, which an export waits for.
let lastUpdate: Promise<void> | undefined;

const enabled = computed(() => open.value && store.isOpen());
const { data: rules } = useQuery(rulesQuery);
const { data: upload } = useQuery(() => ({ ...uploadQuery({ modId: mod.id, key: "" }), enabled: enabled.value }));

const included = computed(() => upload.value?.files?.map((file) => file.rel) ?? []);
const excluded = computed(() => upload.value?.excluded ?? []);
const rows = computed(() => (tab.value === "included" ? included.value : excluded.value));
const tabs = computed(() => [
  { label: `Sent (${describeCount(included.value.length)})`, value: "included" },
  { label: `Left out (${describeCount(excluded.value.length)})`, value: "excluded" },
]);

const { mutate: update } = useMutation({
  mutation: (changes: Partial<ModRecord>) => (lastUpdate = store.updateMod(store.activeId(), mod.id, changes)),
});
const { mutate: importIgnore } = useMutation({
  mutation: () => steam.ReadIgnore(mod.id),
  onSuccess: (text) => {
    if (text) update({ ignore: text });
    else toast.add({ title: "The mod's folder has no .workshop-ignore file to import." });
  },
});
const { mutate: exportIgnore } = useMutation({
  mutation: async () => {
    await lastUpdate?.catch(() => undefined);
    await steam.WriteIgnore(mod.id);
  },
  onSuccess: () =>
    toast.add({ title: "Wrote the patterns to .workshop-ignore in the mod's folder.", color: "success" }),
});
</script>

<template>
  <UModal
    v-model:open="open"
    title="Files never uploaded"
    description="What an upload leaves out of the mod's folder."
    :ui="{ content: 'h-136 sm:max-w-2xl', body: 'flex min-h-0 grow flex-col gap-4 overflow-y-auto' }"
  >
    <template #body>
      <section class="flex flex-col gap-2">
        <span class="text-12 font-medium text-toned">The mod's own patterns</span>
        <UTextarea
          :model-value="mod.ignore"
          :model-modifiers="{ lazy: true }"
          :rows="5"
          placeholder="Gitignore patterns, one per line, such as gfx/source/**"
          class="w-full font-mono"
          @update:model-value="update({ ignore: `${$event}` })"
        />
        <div class="flex gap-1">
          <UButton
            label="Import .workshop-ignore"
            icon="i-lucide-file-down"
            variant="ghost"
            size="sm"
            @click="importIgnore()"
          />
          <UButton
            label="Export .workshop-ignore"
            icon="i-lucide-file-up"
            variant="ghost"
            size="sm"
            @click="exportIgnore()"
          />
        </div>
      </section>

      <section class="flex flex-col gap-1">
        <span class="text-12 font-medium text-toned">Every upload leaves out</span>
        <p v-for="rule in rules?.filter((one) => one.pattern) ?? []" :key="rule.pattern" class="text-12 text-dimmed">
          <span class="font-mono text-toned">{{ rule.pattern }}</span> {{ rule.reason }}
        </p>
      </section>

      <section class="flex min-h-0 flex-col gap-2">
        <span class="text-12 font-medium text-toned">The files of the next upload</span>
        <UTabs :model-value="tab" :items="tabs" :content="false" @update:model-value="tab = `${$event}`" />
        <UScrollArea
          v-slot="{ item }"
          :items="rows"
          :virtualize="{ estimateSize: 24, skipMeasurement: true }"
          class="h-48 shrink-0 rounded-lg border border-default bg-muted"
        >
          <div class="h-6 truncate px-3 font-mono text-12 leading-6 text-toned">{{ item }}</div>
        </UScrollArea>
      </section>
    </template>
  </UModal>
</template>
