<script setup lang="ts">
/**
 * A theme drawn small: a fixed sample of the app in that theme's tokens, its code
 * highlighted as the app highlights code.
 */
import type { ThemeId } from "../../stores/settings";
import ScriptSnippet from "../../ui/ScriptSnippet.vue";

defineProps<{ theme: ThemeId }>();

const SAMPLE = [
  "sunset_invasion.0001 = {",
  "    type = character_event",
  "    title = sir.0001.t",
  "    # envoys arrive",
  "    add_gold = 50",
];
</script>

<template>
  <div :data-theme="theme" class="prev">
    <div class="pv-top">
      <span class="bm" /><span class="tb on">IDE</span><span class="tb">Health</span><span class="tb">Graph</span>
    </div>
    <div class="pv-body">
      <div class="pv-side">
        <i style="box-shadow: inset 2px 0 0 var(--o1)">Sunset Invasion</i>
        <i class="s">sir_events.txt</i>
        <i style="box-shadow: inset 2px 0 0 var(--o2)">SIR Music</i>
        <i style="box-shadow: inset 2px 0 0 var(--game)">Crusader Kings III</i>
      </div>
      <div class="pv-ed">
        <div v-for="(line, i) in SAMPLE" :key="i">
          <b>{{ i + 4 }}</b
          ><ScriptSnippet :text="line" name="sample.txt" inline />
        </div>
      </div>
    </div>
    <div class="pv-foot">
      <span class="pb">Upload</span>
      <span class="bg" style="background: var(--red-soft); color: var(--red)">1</span>
      <span class="bg" style="background: var(--orange-soft); color: var(--orange)">2</span>
      <span class="od" style="background: var(--o1)" />
      <span class="od" style="background: var(--o2)" />
      <span class="od" style="background: var(--o3)" />
      <span class="bg link">link</span>
    </div>
  </div>
</template>

<style scoped>
.prev {
  display: flex;
  flex-direction: column;
  height: 132px;
  overflow: hidden;
  border-radius: 6px;
  border: 1px solid var(--line-2);
  background: var(--bg-1);
  color: var(--fg-1);
  font-size: 9px;
  line-height: 1.45;
}
.pv-top {
  height: 20px;
  flex: none;
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 0 7px;
  background: var(--bg-2);
  border-bottom: 1px solid var(--line-1);
  color: var(--fg-2);
}
.pv-top .bm {
  width: 9px;
  height: 9px;
  border-radius: 3px;
  background: var(--accent);
}
.pv-top .tb {
  height: 20px;
  display: flex;
  align-items: center;
  border-bottom: 1.5px solid transparent;
}
.pv-top .tb.on {
  color: var(--fg-1);
  border-bottom-color: var(--accent);
}
.pv-body {
  flex-grow: 1;
  display: flex;
  min-height: 0;
}
.pv-side {
  width: 76px;
  flex: none;
  background: var(--bg-2);
  border-right: 1px solid var(--line-1);
  padding: 4px 0;
  display: flex;
  flex-direction: column;
  gap: 1px;
}
.pv-side i {
  display: block;
  height: 11px;
  margin-left: 4px;
  font-style: normal;
  color: var(--fg-2);
  padding-left: 5px;
  line-height: 11px;
  white-space: nowrap;
  overflow: hidden;
}
.pv-side i.s {
  background: var(--bg-4);
  color: var(--fg-1);
}
.pv-ed {
  margin: 0;
  flex-grow: 1;
  background: var(--bg-0);
  padding: 4px 0;
  font-family: var(--font-mono);
  font-size: 8.5px;
  line-height: 12px;
  white-space: pre;
  overflow: hidden;
  color: var(--fg-1);
}
.pv-ed b {
  font-weight: 400;
  display: inline-block;
  width: 16px;
  text-align: right;
  padding-right: 5px;
  color: var(--fg-3);
}
.pv-foot {
  height: 22px;
  flex: none;
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 0 6px;
  background: var(--bg-1);
  border-top: 1px solid var(--line-1);
}
.pv-foot .pb {
  height: 13px;
  padding: 0 5px;
  border-radius: 3px;
  background: var(--accent);
  color: var(--accent-ink);
  font-weight: 600;
  font-size: 8px;
  line-height: 13px;
}
.pv-foot .bg {
  height: 12px;
  padding: 0 4px;
  border-radius: 3px;
  font-size: 8px;
  line-height: 12px;
}
.pv-foot .bg.link {
  margin-left: auto;
  color: var(--link);
}
.pv-foot .od {
  width: 6px;
  height: 6px;
  border-radius: 50%;
}
</style>
