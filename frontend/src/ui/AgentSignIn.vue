<!--
  The ways to sign in to an agent, a button each, and the button that says the
  user has signed in.
-->
<script setup lang="ts">
import type { SignIn } from "../stores/agent";

defineProps<{ methods: SignIn[] }>();
defineEmits<{ choose: [method: SignIn]; done: [] }>();
</script>

<template>
  <button
    v-for="method in methods"
    :key="method.id"
    type="button"
    class="flex h-11 items-center gap-2.5 rounded-md border border-accented bg-elevated px-3 text-start"
    @click="$emit('choose', method)"
  >
    <UIcon :name="method.command.length ? 'i-lucide-terminal' : 'i-lucide-log-in'" class="size-4 text-toned" />
    <span class="flex min-w-0 grow flex-col">
      <span class="text-12 font-medium">{{ method.name }}</span>
      <span class="truncate text-11 text-dimmed">{{ method.description }}</span>
    </span>
    <UIcon name="i-lucide-chevron-right" class="size-4 text-dimmed" />
  </button>
  <UButton label="I have signed in" icon="i-lucide-refresh-cw" class="self-start" @click="$emit('done')" />
</template>
