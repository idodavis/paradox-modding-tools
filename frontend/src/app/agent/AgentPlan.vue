<!--
  A session's plan documents, pinned in one row under the pane's header: the
  one the agent last wrote, changed or asks about, with a picker once the
  session has several. The plan's text is never in the transcript.
-->
<script setup lang="ts">
import type { DropdownMenuItem } from "@nuxt/ui";
import { computed, shallowRef } from "vue";

import { loadQuery, placeQuery } from "../../queries";
import { useAgentStore, type Session } from "../../stores/agent";
import { describePlan, type PlanDocument } from "../../stores/agentWork";
import { useEditorStore } from "../../stores/editor";
import CompareView from "../../ui/CompareView.vue";
import { TABLES } from "../../ui/RichEditor.vue";

const { session } = defineProps<{ session: Session }>();

const agent = useAgentStore();
const editor = useEditorStore();
// What the dialog shows of the plan: its text, where it has no file to open
// in the editor, or what the agent changed in it.
const dialog = shallowRef<"" | "text" | "changes">("");

const current = computed(
  () => session.documents.find((document) => document.id === session.shownPlan) ?? session.documents.at(-1),
);
const picker = computed(
  () =>
    session.documents.map((document) => ({
      type: "checkbox" as const,
      label: describePlan(document),
      checked: document === current.value,
      document,
      slot: "plan" as const,
      onUpdateChecked: () => agent.showPlan(session, document.id),
    })) satisfies DropdownMenuItem[],
);

/**
 * describeState returns where a plan stands with the user: asked about,
 * approved, or "" for neither.
 */
function describeState(document: PlanDocument): string {
  if (document.request) return "asks you";

  return document.isApproved ? "approved" : "";
}

/**
 * open shows a plan: its file in an editor tab, in Markdown's rendered view,
 * or its text in a dialog where the agent keeps it in no file the workspace
 * can open.
 */
async function open(document: PlanDocument): Promise<void> {
  agent.showPlan(session, document.id);
  const place = document.path ? await loadQuery(placeQuery(document.path)).catch(() => undefined) : undefined;
  if (!place) {
    dialog.value = "text";
    return;
  }

  editor.show(place);
  editor.showView("visual");
}
</script>

<template>
  <div
    v-if="current"
    class="flex h-8 shrink-0 items-center gap-1.5 border-b border-default ps-2.5 pe-1.5 text-12 whitespace-nowrap"
  >
    <UIcon name="i-lucide-file-text" class="size-3.5 shrink-0 text-dimmed" />
    <span class="shrink-0 text-dimmed">Plan</span>
    <UDropdownMenu v-if="session.documents.length > 1" :items="picker" :content="{ align: 'start' }">
      <UButton
        :label="describePlan(current)"
        trailing-icon="i-lucide-chevron-down"
        variant="ghost"
        color="neutral"
        size="xs"
        class="min-w-0 font-medium"
        :ui="{ label: 'truncate', trailingIcon: 'text-dimmed' }"
        :aria-label="`Choose a plan: ${session.documents.length} in this session`"
      >
        <template #trailing>
          <UBadge :label="session.documents.length" color="neutral" variant="subtle" size="sm" />
          <UIcon name="i-lucide-chevron-down" class="size-3.5 text-dimmed" />
        </template>
      </UButton>
      <template #plan-trailing="{ item }">
        <span class="ms-auto flex items-center gap-1.5 ps-3">
          <UBadge
            v-if="describeState(item.document)"
            :label="describeState(item.document)"
            :color="item.document.isApproved ? 'success' : 'primary'"
            variant="subtle"
            size="sm"
          />
          <UBadge v-if="item.document.previousText" label="updated" color="neutral" variant="subtle" size="sm" />
        </span>
      </template>
    </UDropdownMenu>
    <span v-else class="min-w-0 truncate font-medium" :title="describePlan(current)">{{ describePlan(current) }}</span>
    <UBadge
      v-if="describeState(current)"
      :label="describeState(current)"
      :color="current.isApproved ? 'success' : 'primary'"
      variant="subtle"
      size="sm"
      class="shrink-0"
    />
    <span class="grow" />
    <template v-if="current.previousText">
      <UBadge label="updated" color="neutral" variant="subtle" size="sm" class="shrink-0" />
      <UButton label="Show changes" variant="ghost" color="neutral" size="xs" @click="dialog = 'changes'" />
    </template>
    <UButton label="Open" variant="ghost" color="neutral" size="xs" @click="open(current)" />

    <UModal
      :open="!!dialog"
      :title="describePlan(current)"
      :description="dialog === 'changes' ? 'What the agent changed in the plan' : 'The agent\'s plan'"
      :ui="{ content: 'max-w-4xl', body: 'p-0 sm:p-0' }"
      @update:open="(isOpen) => isOpen || (dialog = '')"
    >
      <template #body>
        <!-- The diff editor fills its box, so the box has its own height. -->
        <div v-if="dialog === 'changes'" class="flex h-[70vh] flex-col">
          <CompareView
            :left="current.previousText"
            :right="current.text"
            language="markdown"
            left-label="Before"
            right-label="Now"
          />
        </div>
        <UEditor
          v-else
          :model-value="current.text"
          content-type="markdown"
          :extensions="TABLES"
          :editable="false"
          :mention="false"
          class="max-h-[70vh] overflow-y-auto px-5 py-4 text-13"
        />
      </template>
    </UModal>
  </div>
</template>
