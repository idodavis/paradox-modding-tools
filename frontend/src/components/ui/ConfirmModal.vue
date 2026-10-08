<script setup lang="ts">
/**
 * A yes-or-no question before an action. Opened through useOverlay, it closes with true when the action is taken; an
 * action that deletes or throws work away is `danger`, in the error color.
 */
const { title, text, action, danger = false } = defineProps<{ title: string; text: string; action: string; danger?: boolean }>();
const emit = defineEmits<{ close: [ok: boolean] }>();
</script>

<template>
  <UModal :title="title" :description="text" @update:open="(open) => !open && emit('close', false)">
    <template #footer>
      <span class="grow" />
      <UButton label="Cancel" @click="emit('close', false)" />
      <UButton :label="action" :color="danger ? 'error' : 'primary'" :variant="danger ? 'soft' : 'solid'" @click="emit('close', true)" />
    </template>
  </UModal>
</template>
