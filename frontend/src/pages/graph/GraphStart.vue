<!-- The page with no root drawn: where to start, the roots drawn last, and the events the workspace's mods define. -->
<script setup lang="ts">
import type { Node } from "@services/graph/models";
import { fileUrl } from "../../queries";
import OriginChip from "../../ui/OriginChip.vue";
import { lookupLook } from "./layout";

const { recent, own } = defineProps<{ recent: Node[]; own: Node[] }>();
const emit = defineEmits<{ draw: [id: string]; showAll: [] }>();

/** How many roots each list shows. */
const SHOWN = 4;
/** The classes of one root's row. */
const ROW = "flex h-8.5 w-full items-center gap-2.5 rounded-md px-2 text-start hover:bg-elevated";
</script>

<template>
  <div class="flex h-full justify-center overflow-auto bg-(--bg-0)">
    <div class="flex w-190 max-w-full flex-col gap-5.5 px-4 pt-24">
      <div class="flex flex-col gap-1.5">
        <span class="text-20 font-semibold text-highlighted">Where to start</span>
        <span class="text-13 text-muted">
          Pick a root to draw what fires it and what it fires. In the editor, <UKbd value="Ctrl" />
          <UKbd value="Shift" /> <UKbd value="G" /> opens the event under the cursor here.
        </span>
      </div>
      <div class="flex items-start gap-7">
        <section v-if="recent.length" class="flex flex-1 flex-col gap-1" aria-label="Recent roots">
          <span class="px-2 pb-1.5 text-11 font-semibold tracking-wide text-dimmed uppercase">Recent</span>
          <button v-for="node in recent.slice(0, SHOWN)" :key="node.id" :class="ROW" @click="emit('draw', node.id)">
            <span
              v-if="node.display === 'window' && node.background"
              class="h-4 w-5.5 shrink-0 rounded-[3px] bg-cover bg-center"
              :style="{ backgroundImage: `url(&quot;${fileUrl(node.background)}&quot;)` }"
            />
            <UIcon
              v-else
              :name="node.display === 'hidden' ? 'i-lucide-eye-off' : lookupLook(node.database).icon"
              class="size-4 shrink-0"
              :style="{ color: lookupLook(node.database).color }"
            />
            <span class="font-mono text-12 text-highlighted">{{ node.name }}</span>
            <span class="min-w-0 grow truncate text-12 text-dimmed">{{ node.title || node.what }}</span>
          </button>
        </section>
        <section v-if="own.length" class="flex flex-1 flex-col gap-1" aria-label="Events from your mods">
          <span class="flex items-center px-2 pb-1.5">
            <span class="text-11 font-semibold tracking-wide text-dimmed uppercase">From your mods</span>
            <span class="grow" />
            <UButton :label="`Show all ${own.length}`" variant="link" size="xs" class="p-0" @click="emit('showAll')" />
          </span>
          <button v-for="node in own.slice(0, SHOWN)" :key="node.id" :class="ROW" @click="emit('draw', node.id)">
            <OriginChip :origin="node.place.origin" dot />
            <span class="font-mono text-12 text-highlighted">{{ node.name }}</span>
            <span class="min-w-0 grow truncate text-12 text-dimmed">{{
              node.display === "hidden" ? "hidden" : node.title || node.what
            }}</span>
            <UBadge
              :label="node.copyOrigin ? 'replaces' : 'added'"
              :color="node.copyOrigin ? 'warning' : 'success'"
              variant="subtle"
              size="sm"
            />
          </button>
        </section>
      </div>
    </div>
  </div>
</template>
