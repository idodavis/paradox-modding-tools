<template>
  <div>
    <label v-if="label" class="block mb-2 font-medium text-sm">{{ label }}</label>
    <p v-if="hint" class="text-xs text-gray-500 mb-1">{{ hint }}</p>
    <Textarea :modelValue="modelValue.join('\n')" @update:modelValue="updatePaths" rows="3" :placeholder="placeholder"
      class="w-full min-w-0 px-3 py-2" />
    <div class="flex flex-wrap gap-2 mt-2">
      <Button :label="fileButtonLabel" @click="selectFiles" size="small" />
      <Button :label="folderButtonLabel" @click="selectFolder" size="small" />
      <Button label="Clear" severity="secondary" @click="clear" size="small" />
    </div>
    <span v-if="modelValue.length" class="text-sm text-gray-400 mt-1 block">
      {{ modelValue.length }} {{ modelValue.length === 1 ? 'item' : 'items' }}
    </span>
  </div>
</template>

<script>
import { SelectDirectory, SelectMultipleFiles } from '../../bindings/paradox-modding-tool/fileservice.js'
import Button from 'primevue/button'
import Textarea from 'primevue/textarea'
import { parsePathList } from '../utils/paths.js'

export default {
  name: 'FileSelector',
  components: { Button, Textarea },
  props: {
    modelValue: {
      type: Array,
      default: () => []
    },
    label: {
      type: String,
      default: 'Files / Folders:'
    },
    placeholder: {
      type: String,
      default: 'Select files or directories...'
    },
    fileDialogTitle: {
      type: String,
      default: 'Select Files to Scan'
    },
    folderDialogTitle: {
      type: String,
      default: 'Select Folder to Scan'
    },
    fileFilter: {
      type: String,
      default: '*.txt; *.json'
    },
    fileButtonLabel: {
      type: String,
      default: 'Select File(s)'
    },
    folderButtonLabel: {
      type: String,
      default: 'Select Folder'
    },
    hint: {
      type: String,
      default: ''
    }
  },
  emits: ['update:modelValue'],
  methods: {
    updatePaths(value) {
      this.$emit('update:modelValue', parsePathList(value))
    },
    async selectFiles() {
      try {
        const selected = await SelectMultipleFiles(this.fileDialogTitle, this.fileFilter)
        if (selected?.length) {
          const existing = new Set(this.modelValue)
          const next = [...this.modelValue]
          for (const p of selected) {
            if (!existing.has(p)) next.push(p)
          }
          this.$emit('update:modelValue', next)
        }
      } catch (e) {
        alert('Error selecting files: ' + e)
      }
    },
    async selectFolder() {
      try {
        const selected = await SelectDirectory(this.folderDialogTitle)
        if (selected) {
          const existing = new Set(this.modelValue)
          if (!existing.has(selected)) {
            this.$emit('update:modelValue', [...this.modelValue, selected])
          }
        }
      } catch (e) {
        alert('Error selecting folder: ' + e)
      }
    },
    clear() {
      this.$emit('update:modelValue', [])
    }
  }
}
</script>
