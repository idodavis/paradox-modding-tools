<!--
  A link an agent asks to open: its address with the host marked, and the
  choice to open or decline it.
-->
<script setup lang="ts">
import { computed } from "vue";

const { agent, message, url } = defineProps<{ agent: string; message: string; url: string }>();
const emit = defineEmits<{ answer: [action: "accept" | "decline" | null] }>();

/** The link split around its host, so the host can stand out. */
const parts = computed(() => {
  try {
    const parsed = new URL(url);
    const start = url.indexOf(parsed.host);

    return {
      before: url.slice(0, start),
      host: parsed.host,
      after: url.slice(start + parsed.host.length),
      lookalike: parsed.hostname.split(".").some((label) => label.startsWith("xn--")),
    };
  } catch {
    return { before: "", host: url, after: "", lookalike: false };
  }
});
</script>

<template>
  <div class="flex max-w-120 flex-col gap-2 rounded-lg border border-default bg-elevated p-3">
    <span class="flex items-center gap-2 text-12 font-medium">
      {{ agent }} asks to open a link
      <span class="grow" />
      <UButton icon="i-lucide-x" variant="ghost" size="xs" square aria-label="Close" @click="emit('answer', null)" />
    </span>
    <span class="text-12">{{ message }}</span>
    <span class="font-mono text-11 break-all text-toned">
      {{ parts.before }}<span class="rounded-xs bg-(--accent-soft) px-0.5 text-highlighted">{{ parts.host }}</span
      >{{ parts.after }}
    </span>
    <span v-if="parts.lookalike" class="text-12 text-warning">
      The host is written with look-alike letters, so it may not be the site it seems to be.
    </span>
    <div class="flex gap-1.5">
      <UButton
        :label="parts.lookalike ? 'Open anyway' : 'Open'"
        icon="i-lucide-external-link"
        :color="parts.lookalike ? 'warning' : 'primary'"
        class="grow justify-center"
        @click="emit('answer', 'accept')"
      />
      <UButton label="Decline" class="grow justify-center" @click="emit('answer', 'decline')" />
    </div>
    <span class="text-11 text-dimmed">Opens in your browser. Nothing is fetched before you choose.</span>
  </div>
</template>
