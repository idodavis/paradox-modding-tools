<!-- A preview of one theme family, light or dark. -->
<script setup lang="ts">
const SIDEBAR = [
  { label: "Sunset Invasion", origin: "--o1" },
  { label: "sir_events.txt" },
  { label: "SIR Music", origin: "--o2" },
  { label: "Crusader Kings III", origin: "--game" },
];

const { family, dark } = defineProps<{ family: string; dark: boolean }>();
</script>

<template>
  <div
    :data-theme="`${family}-${dark ? 'dark' : 'light'}`"
    class="flex h-33 flex-col overflow-hidden rounded-md border border-(--line-2) bg-(--bg-1) text-[9px] leading-[1.45] text-(--fg-1)"
  >
    <div class="flex h-5 flex-none items-center gap-2 border-b border-(--line-1) bg-(--bg-2) px-1.75 text-(--fg-2)">
      <span class="size-2.25 rounded-[3px] bg-(--accent)" />
      <span class="flex h-5 items-center border-b-[1.5px] border-(--accent) text-(--fg-1)">IDE</span>
      <span>Health</span>
      <span>Graph</span>
    </div>
    <div class="flex min-h-0 grow">
      <div class="flex w-19 flex-none flex-col gap-px border-e border-(--line-1) bg-(--bg-2) py-1 text-(--fg-2)">
        <span
          v-for="row in SIDEBAR"
          :key="row.label"
          class="ms-1 block h-2.75 truncate ps-1.25 leading-[11px]"
          :class="{ 'bg-(--bg-4) text-(--fg-1)': !row.origin }"
          :style="{ boxShadow: row.origin && `inset 2px 0 0 var(${row.origin})` }"
        >
          {{ row.label }}
        </span>
      </div>
      <pre
        class="m-0 grow overflow-hidden bg-(--bg-0) py-1 font-mono text-[8.5px] leading-3"
      ><span class="text-(--fg-3)">4  </span><span class="text-(--syn-entry)">sunset_invasion.0001</span> <span class="text-(--syn-operator)">=</span> {
<span class="text-(--fg-3)">5  </span>    <span class="text-(--syn-block)">trigger</span> = { <span class="text-(--syn-field)">is_adult</span> = <span class="text-(--syn-literal)">yes</span> }
<span class="text-(--fg-3)">6  </span>    <span class="text-(--syn-comment)"># envoys arrive</span>
<span class="text-(--fg-3)">7  </span>    <span class="text-(--syn-scope)">scope:envoy</span> = { <span class="text-(--syn-field)">add_gold</span> = <span class="text-(--syn-number)">50</span> }
<span class="text-(--fg-3)">8  </span>    <span class="text-(--syn-field)">title</span> = <span class="text-(--syn-string)">sir.0001.t</span></pre>
    </div>
    <div class="flex h-5.5 flex-none items-center gap-1 border-t border-(--line-1) bg-(--bg-1) px-1.5">
      <span class="rounded-[3px] bg-(--accent) px-1.25 text-[8px] leading-[13px] font-semibold text-(--accent-ink)">
        Upload
      </span>
      <span class="rounded-[3px] bg-(--red-soft) px-1 text-[8px] text-(--red)">1</span>
      <span class="rounded-[3px] bg-(--orange-soft) px-1 text-[8px] text-(--orange)">2</span>
      <span v-for="n in 3" :key="n" class="size-1.5 rounded-full" :style="{ background: `var(--o${n})` }" />
      <span class="ms-auto px-1 text-[8px] text-(--link)">link</span>
    </div>
  </div>
</template>
