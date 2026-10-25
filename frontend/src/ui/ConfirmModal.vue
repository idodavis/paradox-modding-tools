<!--
  A confirmation dialog with a title, a description and its buttons. It resolves with the choice made: true for the
  confirm, "alternative" for the second action when it has one, and false for Cancel. A destructive confirm is red.
-->
<script setup lang="ts">
const {
  title,
  body,
  confirm,
  alternative = "",
  destructive = false,
} = defineProps<{
  title: string;
  body: string;
  confirm: string;
  alternative?: string;
  destructive?: boolean;
}>();
const emit = defineEmits<{ close: [choice: boolean | "alternative"] }>();
</script>

<template>
  <UModal :title="title" :description="body" @update:open="(open) => !open && emit('close', false)">
    <template #footer>
      <span class="grow" />
      <UButton label="Cancel" @click="emit('close', false)" />
      <UButton v-if="alternative" :label="alternative" @click="emit('close', 'alternative')" />
      <UButton
        :label="confirm"
        :color="destructive ? 'error' : 'primary'"
        :variant="destructive ? 'soft' : 'solid'"
        @click="emit('close', true)"
      />
    </template>
  </UModal>
</template>
