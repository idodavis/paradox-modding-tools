<template>
  <div>
    <label class="block mb-2 font-medium text-sm">Object Types:</label>
    <div class="flex flex-wrap gap-2 items-center">
      <MultiSelect v-model="selected" :options="types" :loading="loading" placeholder="Select object types"
        :maxSelectedLabels="3" class="w-full md:w-96" filter />
      <Button label="All" @click="selectAll" size="small" severity="secondary" text />
      <Button label="None" @click="selectNone" size="small" severity="secondary" text />
    </div>
    <p v-if="types.length > 0" class="text-xs text-gray-400 mt-1">
      {{ types.length }} types available, {{ selected.length }} selected
    </p>
    <p v-if="error" class="text-xs text-red-400 mt-1">
      Error loading types: {{ error }}
    </p>
  </div>
</template>

<script>
import { GetSupportedTypes } from '../../../bindings/paradox-modding-tool/inventoryservice.js'
import MultiSelect from 'primevue/multiselect'
import Button from 'primevue/button'

export default {
  name: 'TypeSelector',
  components: { MultiSelect, Button },
  props: {
    modelValue: {
      type: Array,
      default: () => []
    },
    game: {
      type: String,
      required: true
    }
  },
  emits: ['update:modelValue'],
  data() {
    return {
      types: [],
      loading: false,
      error: null
    }
  },
  computed: {
    selected: {
      get() {
        return this.modelValue
      },
      set(value) {
        this.$emit('update:modelValue', value)
      }
    }
  },
  watch: {
    game: {
      immediate: true,
      async handler(newGame) {
        if (newGame) {
          await this.loadTypes()
        }
      }
    }
  },
  methods: {
    async loadTypes() {
      this.loading = true
      this.error = null
      try {
        console.log('Loading types for game:', this.game)
        this.types = await GetSupportedTypes(this.game)
        console.log('Loaded types:', this.types)
        // Clear selection when types change
        this.$emit('update:modelValue', [])
      } catch (error) {
        console.error('Failed to load types:', error)
        this.error = String(error)
        this.types = []
      } finally {
        this.loading = false
      }
    },
    selectAll() {
      this.$emit('update:modelValue', [...this.types])
    },
    selectNone() {
      this.$emit('update:modelValue', [])
    }
  }
}
</script>
