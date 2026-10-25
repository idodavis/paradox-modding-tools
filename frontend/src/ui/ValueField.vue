<!--
  One value edited by its kind: a switch, a choice, a number, a text, a color or a set of toggles, with its label and
  help. The caller writes the value where it belongs.
-->
<script setup lang="ts">
import { computed } from "vue";

const {
  kind,
  value,
  label,
  help = "",
  choices = [],
  min = 0,
  max = 0,
  step = 1,
  disabled = false,
} = defineProps<{
  kind: "switch" | "choice" | "number" | "text" | "color" | "toggles";
  value: string;
  /** What the value is called. With none, the field is its control alone, small. */
  label: string;
  help?: string;
  /** The values a choice offers, or the toggles of a set. A set's value holds the toggles on, separated by spaces. */
  choices?: readonly string[];
  min?: number;
  max?: number;
  step?: number;
  disabled?: boolean;
}>();
const emit = defineEmits<{ "update:value": [value: string] }>();

const range = computed(() => (min || max ? { min, max } : {}));
const size = computed(() => (label ? "md" : "xs"));
const toggled = computed(() => value.split(" ").filter(Boolean));

/** Emits the toggles on once the given one is switched. */
function toggle(choice: string): void {
  const on = toggled.value.includes(choice)
    ? toggled.value.filter((one) => one !== choice)
    : [...toggled.value, choice];
  emit("update:value", on.toSorted().join(" "));
}
</script>

<template>
  <UFormField
    :label="label"
    :description="help"
    :orientation="label ? 'horizontal' : 'vertical'"
    :ui="label ? { container: 'flex justify-end sm:w-52' } : undefined"
  >
    <USwitch
      v-if="kind === 'switch'"
      :model-value="value === 'true'"
      :disabled="disabled"
      @update:model-value="(on) => emit('update:value', String(on))"
    />
    <USelect
      v-else-if="kind === 'choice'"
      :model-value="value"
      :items="choices.map((choice) => ({ label: choice.charAt(0).toUpperCase() + choice.slice(1), value: choice }))"
      :size="size"
      :disabled="disabled"
      class="w-full"
      @update:model-value="(choice) => emit('update:value', String(choice))"
    />
    <UInputNumber
      v-else-if="kind === 'number'"
      :model-value="Number(value)"
      v-bind="range"
      :step="step"
      :size="size"
      :increment="!!label"
      :decrement="!!label"
      :disabled="disabled"
      class="w-full"
      @update:model-value="(number) => number !== null && emit('update:value', String(number))"
    />
    <UInput
      v-else-if="kind === 'text'"
      :model-value="value"
      :model-modifiers="{ lazy: true }"
      :size="size"
      :disabled="disabled"
      class="w-full"
      @update:model-value="(text) => emit('update:value', String(text))"
    />
    <UPopover v-else-if="kind === 'color'">
      <UButton
        color="neutral"
        variant="outline"
        :size="size"
        :disabled="disabled"
        class="w-full justify-start font-mono"
        :label="value || 'None'"
      >
        <template #leading>
          <span class="size-3.5 rounded-sm border border-default" :style="{ background: value || 'transparent' }" />
        </template>
      </UButton>
      <template #content>
        <UColorPicker
          class="p-2"
          :model-value="value"
          :throttle="250"
          @update:model-value="(color) => color && emit('update:value', color)"
        />
      </template>
    </UPopover>
    <UFieldGroup v-else size="xs">
      <UButton
        v-for="choice in choices"
        :key="choice"
        :label="choice"
        :color="toggled.includes(choice) ? 'primary' : 'neutral'"
        :variant="toggled.includes(choice) ? 'soft' : 'outline'"
        :aria-pressed="toggled.includes(choice)"
        :disabled="disabled"
        @click="toggle(choice)"
      />
    </UFieldGroup>
  </UFormField>
</template>
