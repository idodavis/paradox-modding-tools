<script setup lang="ts">
/**
 * Workspace IDE toolbar; workbench fills App.vue host below this strip, booted by the toolbar.
 */
import { computed } from "vue";
import { useRoute } from "vue-router";
import WorkspaceToolBar from "../components/WorkspaceToolBar.vue";
import LanguageHealthStrip from "../components/LanguageHealthStrip.vue";
import { guideOpen, setGuideOpen } from "../composables/useGuidePrefs";

defineOptions({ name: "WorkspaceIdePage" });

const route = useRoute();

const workspaceId = computed(() => String(route.params.id ?? ""));
</script>

<template>
  <WorkspaceToolBar :workspace-id="workspaceId">
    <template #trailing>
      <UButton
        icon="i-lucide-book-open"
        label="Guide"
        size="xs"
        color="neutral"
        :variant="guideOpen ? 'soft' : 'ghost'"
        @click="setGuideOpen(!guideOpen)"
      />
      <LanguageHealthStrip v-if="workspaceId" :workspace-id="workspaceId" />
    </template>
  </WorkspaceToolBar>
</template>
