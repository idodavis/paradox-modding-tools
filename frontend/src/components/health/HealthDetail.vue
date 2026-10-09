<script setup lang="ts">
/**
 * The open row (the Health board's right pane): its name with its kind and rule, what the rule does, then
 * each place it is written as a site card with its lines; an untranslated key's file in the language it
 * goes in, and what reads it. A contest compares the kept definition's file with the one it replaces.
 */
import { computed } from "vue";
import { useQuery } from "@pinia/colada";
import type { File as SourceFile } from "@services/internal/lang/source/models";
import { sitesOf, typeOf, type Row } from "../../composables/useHealth";
import { linesQuery, locReadersQuery } from "../../queries";
import { useWorkspaceStore } from "../../stores/workspace";
import CompareFiles from "../ui/CompareFiles.vue";
import { list } from "../../lib/format";

const { workspaceId, row, sources, live } = defineProps<{
  workspaceId: string;
  row: Row | null;
  sources: SourceFile[];
  live: boolean;
}>();

const store = useWorkspaceStore();
const compare = useOverlay().create(CompareFiles);

const contest = computed(() => (row && "rule" in row ? row : null));
// a whole file one origin ships at another's path: its sites list what each file defines, not lines
const replacedFile = computed(() => contest.value?.database === "file");
const sites = computed(() => (row ? sitesOf(row).map((site) => ({ ...site, at: sources[site.file] })) : []));
const { data: lines } = useQuery(() =>
  linesQuery({
    id: workspaceId,
    places: replacedFile.value
      ? []
      : sites.value.map((site) => ({
          path: site.at.path,
          line: site.line,
          col: site.col,
          definition: !!contest.value,
        })),
    live,
  }),
);
const { data: readers } = useQuery(() =>
  locReadersQuery({ id: workspaceId, key: row && "key" in row ? row.key : "", live }),
);

const title = computed(() => {
  if (!row) return "";
  if ("message" in row) return row.message;
  if ("key" in row) return row.key;
  if ("name" in row) return row.name;
  return `${store.originName(row.from)} ${row.affected ? "is affected by" : "depends on"} ${store.originName(row.to)}`;
});
// a problem row's type in its color, then what kind of name it is or the language it is fixed in
const type = computed(() => (row && ("code" in row || "key" in row) ? typeOf(row) : null));
const tag = computed(() => (row && (("kind" in row && row.kind) || ("language" in row && row.language))) || "");

const names = computed(() => sites.value.map((site) => store.originName(site.at.origin)));
/** Each site's part in a contest: kept, added to the kept one, or replaced by it. */
function part(i: number): "kept" | "injected" | "replaced" {
  if (i === contest.value?.winner) return "kept";
  return sites.value[i].injected ? "injected" : "replaced";
}
// the kept definition first, as the board draws it, then the rest in load order
const ordered = computed(() =>
  sites.value
    .map((site, i) => ({ site, i }))
    .sort((a, b) => Number(b.i === contest.value?.winner) - Number(a.i === contest.value?.winner)),
);
/** A site's lines without Go's closing "…" (a definition past ten lines), which is drawn apart, unnumbered. */
function linesOf(i: number): { text: string; more: boolean } {
  const read = lines.value?.[i] ?? [];
  const more = read.at(-1) === "…";
  return { text: (more ? read.slice(0, -1) : read).join("\n"), more };
}
const replacedMod = computed(
  () =>
    !!contest.value && sites.value.some((site, i) => part(i) === "replaced" && site.at.origin !== store.originVanilla),
);
// the replaced definition nearest the kept one in load order, which Compare the two sets beside it
const nearest = computed(() => {
  const kept = contest.value?.winner ?? -1;
  let best = -1;
  sites.value.forEach((_, i) => {
    if (kept >= 0 && part(i) === "replaced" && (best < 0 || Math.abs(i - kept) < Math.abs(best - kept))) best = i;
  });
  return best;
});

/** The names a replaced file defines that the kept file does not. */
function dropped(i: number): string[] {
  const kept = sites.value[contest.value?.winner ?? -1]?.defines ?? [];
  return part(i) === "replaced" ? (sites.value[i].defines ?? []).filter((name) => !kept.includes(name)) : [];
}

// what the contest's rule does, in the names of the origins it is between ("A, B, and C")
const rule = computed(() => {
  const row = contest.value;
  if (!row || row.winner < 0) return "";
  const kept = names.value[row.winner];
  const others = (by: ReturnType<typeof part>) => list(new Set(names.value.filter((_, i) => part(i) === by)));
  if (replacedFile.value)
    return `The game loads one file per path, so ${kept}'s file replaces ${others("replaced")}'s whole.`;
  if (others("injected"))
    return `The game runs ${kept}'s definition together with what ${others("injected")} injected into it.`;
  if (row.rule !== "FIOS" && row.rule !== "LIOS")
    return `${kept} writes it as ${row.rule}, so its version takes the place of ${others("replaced")}'s.`;
  const [order, loads] = row.firstWins ? ["first", "before"] : ["last", "after"];
  return `For this kind the ${order} mod in load order wins. ${kept} loads ${loads} ${others("replaced")}, so its version is the one the game runs.`;
});

function openCompare(): void {
  const at = (i: number) => ({ ...sites.value[i].at, line: sites.value[i].line });
  if (contest.value && nearest.value >= 0) {
    void compare.open({
      title: "Compare the two",
      tags: ["Replaced", "Kept"],
      old: at(nearest.value),
      now: at(contest.value.winner),
    });
  }
}
</script>

<template>
  <section class="flex min-w-0 grow flex-col bg-default">
    <p v-if="!row" class="p-4 text-12 text-dimmed">Select a row.</p>
    <template v-else>
      <div class="flex flex-col gap-2 border-b border-default px-4 py-3">
        <div class="flex items-center gap-2">
          <span class="truncate font-mono text-13 font-semibold">{{ title }}</span>
          <UBadge v-if="type" :label="type.label" :color="type.color" size="sm" />
          <UBadge v-if="tag" :label="tag" size="sm" />
          <UBadge v-if="contest" :label="contest.rule" :color="contest.firstWins ? 'warning' : 'info'" size="sm" />
          <UBadge v-if="replacedMod" label="Replaces a mod's own" color="error" size="sm" />
          <span class="grow" />
          <UButton v-if="nearest >= 0" label="Compare the two" icon="i-lucide-diff" @click="openCompare" />
        </div>
        <p v-if="rule" class="text-12 text-muted">{{ rule }}</p>
      </div>
      <div class="flex min-h-0 flex-col gap-3 overflow-auto px-4 py-3">
        <div
          v-for="{ site, i } in ordered"
          :key="`${site.at.path}:${site.line}:${site.col}`"
          class="shrink-0 overflow-hidden rounded-lg border border-default bg-(--bg-0)"
        >
          <div class="flex h-8 items-center gap-2 border-b border-default bg-elevated px-2.5 text-12 whitespace-nowrap">
            <OriginDot :color="store.originColor(site.at.origin)" small />
            <span class="font-medium">{{ names[i] }}</span>
            <span v-if="site.name" class="font-mono text-dimmed">{{ site.name }}</span>
            <SourceLink :at="site.at" :line="site.line" :file="replacedFile" />
            <span class="grow" />
            <UBadge
              v-if="site.removed"
              :label="`Removed by ${row && 'to' in row ? store.originName(row.to) : ''}`"
              color="error"
              size="sm"
            />
            <UBadge v-else-if="part(i) === 'kept' && contest" label="Kept" color="success" size="sm" />
            <UBadge v-else-if="part(i) === 'injected'" label="Injected" color="info" size="sm" />
            <UBadge v-else-if="contest" label="Replaced" size="sm" />
          </div>
          <template v-if="lines?.[i]?.length">
            <ScriptSnippet
              :text="linesOf(i).text"
              :name="site.at.rel"
              :from-line="site.line"
              class="py-1.5"
              :class="{ 'opacity-70': contest && part(i) === 'replaced' }"
            />
            <p v-if="linesOf(i).more" class="ps-12 pb-1.5 font-mono text-12 text-dimmed">…</p>
          </template>
          <div v-else-if="site.defines?.length" class="flex flex-col gap-1.5 px-2.5 py-2">
            <span class="text-11 text-dimmed">
              Defines {{ site.defines.length
              }}<template v-if="site.changed?.length"
                >; <span class="text-info">{{ site.changed.length }} written differently</span></template
              ><template v-if="dropped(i).length"
                >; <span class="text-warning">the kept file leaves out {{ dropped(i).length }}</span></template
              >
            </span>
            <div
              class="flex flex-wrap gap-x-3 gap-y-0.5 font-mono text-12"
              :class="{ 'opacity-70': part(i) === 'replaced' }"
            >
              <span
                v-for="name in site.defines"
                :key="name"
                :class="{ 'text-info': site.changed?.includes(name), 'text-warning': dropped(i).includes(name) }"
                >{{ name }}</span
              >
            </div>
          </div>
        </div>
        <p v-if="row && 'target' in row" class="flex min-w-0 items-center gap-1.5 text-12 text-muted">
          In {{ row.language }} it goes in
          <SourceLink :at="sources[row.target]" :line="0" file />
        </p>
        <section v-if="readers?.length" class="flex flex-col items-start gap-1">
          <h4 class="text-12 font-semibold">Read by</h4>
          <SourceLink
            v-for="reader in readers"
            :key="`${reader.at.path}:${reader.line}:${reader.col}`"
            :at="reader.at"
            :line="reader.line"
          />
        </section>
      </div>
    </template>
  </section>
</template>
