<template>
  <div>
    <label class="block mb-2 font-medium text-sm">Object Types:</label>
    <div class="flex flex-wrap gap-2 items-center">
      <MultiSelect v-model="selected" :options="types" :loading="loading" placeholder="Select object types"
        :maxSelectedLabels="3" class="w-full md:w-96" filter />
      <Button label="All" @click="selectAll" size="small" severity="secondary" text />
      <Button label="None" @click="selectNone" size="small" severity="secondary" text />
    </div>
    <p v-if="types.length > 0" class="text-xs text-(--p-surface-400) mt-1">
      {{ types.length }} types available, {{ selected.length }} selected
    </p>
    <p v-if="error" class="text-xs text-red-400 mt-1">
      Error loading types: {{ error }}
    </p>
  </div>
</template>

<script setup>
import { watch, ref, computed } from 'vue'
import { GetSupportedTypes } from '../../../bindings/paradox-modding-tool/inventoryservice.js'

const props = defineProps({
  modelValue: {
    type: Array,
    default: () => []
  },
  game: {
    type: String,
    required: true
  }
})

const emit = defineEmits(['update:modelValue'])

const types = ref([])
const loading = ref(false)
const error = ref(null)

const selected = computed({
  get: () => props.modelValue,
  set: (value) => emit('update:modelValue', value)
})

async function loadTypes() {
  loading.value = true
  error.value = null
  try {
    console.log('Loading types for game:', props.game)
    types.value = await GetSupportedTypes(props.game)
    console.log('Loaded types:', types.value)
    emit('update:modelValue', [])
  } catch (err) {
    console.error('Failed to load types:', err)
    error.value = String(err)
    types.value = []
  } finally {
    loading.value = false
  }
}

watch(() => props.game, loadTypes, { immediate: true })

function selectAll() {
  emit('update:modelValue', [...types.value])
}

function selectNone() {
  emit('update:modelValue', [])
}
</script>
