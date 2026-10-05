<script setup lang="ts">
/**
 * The IDE page's strip over the workbench, which App keeps mounted behind every page: the Guide and the
 * IDE's settings. The open file's facts go to the status bar.
 */
import { guideOpen, setGuideOpen } from "../composables/useGuidePrefs";

defineProps<{ id: string }>();
</script>

<template>
  <div class="flex h-8 shrink-0 items-center justify-end gap-1.5 border-b border-default bg-muted px-2">
    <UButton
      icon="i-lucide-book-open"
      label="Guide"
      size="xs"
      :variant="guideOpen ? 'soft' : 'ghost'"
      @click="setGuideOpen(!guideOpen)"
    />
    <IdeSettingsCard />
  </div>
  <StatusFacts>
    <IdeStatusStrip />
  </StatusFacts>
</template>
