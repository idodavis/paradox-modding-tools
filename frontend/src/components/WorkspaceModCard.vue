<script setup lang="ts">
/**
 * One mod of a workspace (the Settings board's detail card): its name and folder, color, thumbnail and
 * calendar, then its publishing, who reads it, and what its uploads leave out. Each change saves at once.
 */
import { computed, shallowRef } from "vue";
import { useMutation, useQuery, useQueryCache } from "@pinia/colada";
import type { WorkspaceMod } from "@services/models";
import type { Era } from "@services/internal/session/models";
import { CopyThumbnail, SaveListing } from "@services/releaseservice";
import {
  ExportWorkshopIgnore,
  ImportWorkshopIgnore,
  OpenFolder,
  RemoveWorkspaceMod,
  SetWorkspaceModCalendar,
  SetWorkspaceModPath,
  SetWorkspaceModSlot,
  UpdateWorkspaceMod,
} from "@services/workspaceservice";
import { IMAGE_FILTER, pickDirectory, pickFile } from "../lib/nativeDialog";
import {
  healthQuery,
  imageQuery,
  listingQuery,
  releaseKeys,
  sessionKeys,
  statQuery,
  uploadFilesQuery,
  workspaceKeys,
  workspacesQuery,
} from "../queries";
import { modColor, useWorkspaceStore } from "../stores/workspace";

const { workspaceId, mod, live = false } = defineProps<{ workspaceId: string; mod: WorkspaceMod; live?: boolean }>();
const emit = defineEmits<{ removed: [] }>();

const store = useWorkspaceStore();
const queryCache = useQueryCache();
const ids = computed(() => ({ workspaceId, modId: mod.id }));
const { data: listing } = useQuery(() => listingQuery(ids.value));
const { data: upload } = useQuery(() => uploadFilesQuery(ids.value));
const { data: health } = useQuery(() => healthQuery({ id: workspaceId, live }));
const thumbnail = computed(() => mod.thumbnail || listing.value?.thumbnailAbs || "");
const { data: image } = useQuery(() => imageQuery(thumbnail.value));
const { data: stat } = useQuery(() => statQuery(thumbnail.value));

/** Each change to the mod, saved; one that rebuilds the session says so. */
const { mutate: run } = useMutation({
  mutation: ({ save }: { save: () => Promise<unknown>; rebuilds?: boolean }) => save(),
  onSettled: (_data, _error, { rebuilds }) => {
    void queryCache.invalidateQueries({ key: workspaceKeys.all });
    void queryCache.invalidateQueries({ key: releaseKeys.listing(workspaceId, mod.id) });
    void queryCache.invalidateQueries({ key: releaseKeys.upload(workspaceId, mod.id) });
    if (rebuilds) void queryCache.invalidateQueries({ key: sessionKeys.live(workspaceId) });
  },
});

/** Show a change at once: the next save of another field sends it too. */
function shown(next: WorkspaceMod): void {
  queryCache.setQueryData(workspacesQuery.key, (list) =>
    (list ?? []).map((ws) => (ws.id !== workspaceId ? ws : { ...ws, mods: (ws.mods ?? []).map((m) => (m.id === next.id ? next : m)) })),
  );
}

type Fields = Partial<Pick<WorkspaceMod, "name" | "color" | "thumbnail" | "descMdRel" | "descBbRel" | "workshopIgnore">>;
function saveFields(set: Fields): void {
  const next = { ...mod, ...set };
  shown(next);
  run({
    save: () =>
      UpdateWorkspaceMod(workspaceId, mod.id, next.name, next.color ?? "", next.thumbnail ?? "", next.descMdRel ?? "",
        next.descBbRel ?? "", next.workshopIgnore ?? ""),
  });
}

/** A text field's value once it is left. */
function typed(event: Event): string {
  return event.target instanceof HTMLInputElement || event.target instanceof HTMLTextAreaElement ? event.target.value : "";
}

const { mutate: openFolder } = useMutation({ mutation: () => OpenFolder(mod.path) });

function setSlot(slot: number): void {
  shown({ ...mod, slot, color: "" });
  run({ save: () => SetWorkspaceModSlot(workspaceId, mod.id, slot) });
}

async function changeFolder(): Promise<void> {
  const path = await pickDirectory("The mod's folder");
  if (path) run({ save: () => SetWorkspaceModPath(workspaceId, mod.id, path), rebuilds: true });
}

function remove(): void {
  run({ save: () => RemoveWorkspaceMod(workspaceId, mod.id), rebuilds: true });
  emit("removed");
}

// the listing's image, which the launcher and Steam show, is PMT's too
async function changeThumbnail(): Promise<void> {
  const src = await pickFile("Thumbnail", IMAGE_FILTER);
  const current = listing.value;
  if (!src || !current) return;
  run({
    save: async () => {
      const rel = await CopyThumbnail(workspaceId, mod.id, src);
      const saved = await SaveListing({ ...current, thumbnailRel: rel });
      await UpdateWorkspaceMod(workspaceId, mod.id, mod.name, mod.color ?? "", saved?.thumbnailAbs ?? "",
        mod.descMdRel ?? "", mod.descBbRel ?? "", mod.workshopIgnore ?? "");
    },
  });
}

const size = computed(() => {
  if (!image.value || !stat.value?.exists) return "";
  return `${image.value.width} by ${image.value.height}, ${Math.round(stat.value.size / 1024)} KB`;
});

const eras = computed(() => mod.eras ?? []);
function setEras(next: Era[]): void {
  shown({ ...mod, eras: next });
  run({ save: () => SetWorkspaceModCalendar(workspaceId, mod.id, next), rebuilds: true });
}
function setEra(at: number, set: Partial<Era>): void {
  setEras(eras.value.map((era, i) => (i === at ? { ...era, ...set } : era)));
}
// Go keeps only named eras, so a new one waits here until it has a name
const blank = shallowRef<Era>();
function editBlank(set: Partial<Era>): void {
  const next = { name: "", offset: 0, ...blank.value, ...set };
  if (!next.name.trim()) {
    blank.value = next;
    return;
  }
  blank.value = undefined;
  setEras([...eras.value, next]);
}

// an example works out the offset: a year the mod writes and the year a player sees (AGoT writes
// 8082 for 82, so 8000); only unparseable input gives none
const exWritten = shallowRef("");
const exSeen = shallowRef("");
const exEra = shallowRef("");
const workedOut = computed(() => {
  const yearOf = (raw: string) => {
    const head = raw.trim().split(".")[0];
    const year = Number(head);
    return head && head !== "-" && Number.isFinite(year) ? Math.trunc(year) : undefined;
  };
  const written = yearOf(exWritten.value);
  const seen = yearOf(exSeen.value);
  return written === undefined || seen === undefined ? undefined : written - seen;
});

function unlink(): void {
  const current = listing.value;
  if (current) run({ save: () => SaveListing({ ...current, remoteFileId: "" }) });
}

/** Who reads this mod's objects, and how much of the game it replaces. */
const usage = computed(() => {
  const report = health.value;
  if (!report) return undefined;
  const origin = (file?: number) => (file === undefined ? "" : report.sources?.[file]?.origin);
  const readers = new Map<string, Set<string>>();
  for (const row of report.depends ?? []) {
    if (row.to !== mod.id) continue;
    const names = readers.get(row.from) ?? new Set<string>();
    for (const site of row.sites ?? []) names.add(`${site.database} ${site.name}`);
    readers.set(row.from, names);
  }
  const replaced = (report.overrides ?? []).filter(
    (row) =>
      origin(row.sites?.[row.winner]?.file) === mod.id && (row.sites ?? []).some((site) => origin(site.file) === store.originVanilla),
  ).length;
  return { readers: [...readers].map(([from, names]) => ({ from, count: names.size })), replaced };
});

const patterns = computed(() => (mod.workshopIgnore ?? "").split("\n").filter((line) => line.trim()));
const editing = shallowRef(false);
const draft = shallowRef("");
function edit(open: boolean): void {
  editing.value = open;
  if (open) draft.value = mod.workshopIgnore ?? "";
}
function saveExclusions(): void {
  saveFields({ workshopIgnore: draft.value });
  editing.value = false;
}
const { mutate: importExclusions } = useMutation({
  mutation: () => ImportWorkshopIgnore(workspaceId, mod.id),
  onSuccess: (text) => (draft.value = text),
  onSettled: () => queryCache.invalidateQueries({ key: workspaceKeys.all }),
});
function exportExclusions(): void {
  saveFields({ workshopIgnore: draft.value });
  run({ save: () => ExportWorkshopIgnore(workspaceId, mod.id) });
}
</script>

<template>
  <div class="flex min-w-0 flex-1 flex-col overflow-hidden rounded-lg border border-default bg-muted">
    <div class="flex items-center gap-3.5 border-b border-default px-5 py-4">
      <Thumb :path="thumbnail" :color="modColor(mod)" size="lg" />
      <div class="flex min-w-0 grow flex-col gap-0.5">
        <span class="truncate text-16">{{ mod.name }}</span>
        <span class="truncate font-mono text-12 text-dimmed">{{ mod.folder }}</span>
      </div>
      <UButton label="Open folder" icon="i-lucide-folder" :disabled="mod.isBroken" @click="openFolder()" />
      <UButton label="Remove from workspace" variant="ghost" class="text-error" @click="remove" />
    </div>

    <div class="grid min-h-0 flex-1 grid-cols-2 overflow-y-auto">
      <div class="flex flex-col border-e border-default">
        <FieldGroup title="Identity">
          <UFormField label="Name">
            <UInput :model-value="mod.name" class="w-full" @change="saveFields({ name: typed($event) })" />
          </UFormField>
          <UFormField label="Folder">
            <div class="flex gap-2">
              <UInput :model-value="mod.folder" readonly class="grow" :ui="{ base: 'font-mono' }" />
              <UButton label="Change" size="md" @click="changeFolder" />
            </div>
          </UFormField>
        </FieldGroup>

        <FieldGroup title="Color" :hint="mod.color ? 'Picked by hand' : `Slot ${mod.slot}, set automatically`">
          <OriginColorField :model-value="mod.color ?? ''" :slot="mod.slot" @update:model-value="saveFields({ color: $event })" @slot="setSlot" />
        </FieldGroup>

        <FieldGroup title="Thumbnail" hint="Shown in the launcher, on Steam and in PMT">
          <div class="flex items-end gap-3.5">
            <Thumb :path="thumbnail" :color="modColor(mod)" size="xl" />
            <div class="flex min-w-0 flex-col items-start gap-1.5">
              <span class="truncate font-mono text-12 text-toned">{{ listing?.thumbnailRel || "No thumbnail" }}</span>
              <span v-if="size" class="text-12 text-dimmed">{{ size }}</span>
              <UButton label="Change" @click="changeThumbnail" />
            </div>
          </div>
        </FieldGroup>

        <FieldGroup title="Calendar" hint="Shows event dates as in-game years">
          <div v-for="(era, i) in eras" :key="i" class="flex items-end gap-2.5">
            <UFormField label="Era" class="w-30">
              <UInput :model-value="era.name" placeholder="AD" @change="setEra(i, { name: typed($event) })" />
            </UFormField>
            <UFormField label="Year offset" class="w-30">
              <UInput type="number" :model-value="era.offset" @change="setEra(i, { offset: Number(typed($event)) || 0 })" />
            </UFormField>
            <UButton
              icon="i-lucide-x"
              variant="ghost"
              size="md"
              square
              aria-label="Remove this era"
              @click="setEras(eras.filter((_, at) => at !== i))"
            />
          </div>
          <div v-if="blank" class="flex items-end gap-2.5">
            <UFormField label="Era" class="w-30">
              <UInput :model-value="blank.name" placeholder="AD" autofocus @change="editBlank({ name: typed($event) })" />
            </UFormField>
            <UFormField label="Year offset" class="w-30">
              <UInput type="number" :model-value="blank.offset" @change="editBlank({ offset: Number(typed($event)) || 0 })" />
            </UFormField>
            <UButton icon="i-lucide-x" variant="ghost" size="md" square aria-label="Remove this era" @click="blank = undefined" />
          </div>
          <div class="flex items-center gap-2">
            <UButton label="Add era" icon="i-lucide-plus" variant="ghost" size="md" :disabled="!!blank" @click="blank = { name: '', offset: 0 }" />
            <UPopover :content="{ align: 'start' }">
              <UButton label="Work it out" variant="ghost" size="md" />
              <template #content>
                <div class="flex w-80 flex-col gap-3 p-3.5">
                  <p class="text-12 text-dimmed">
                    A total conversion offsets its years: A Game of Thrones writes 8082.1.1 for 82 AC. Give a date the mod
                    writes and the year a player sees.
                  </p>
                  <div class="grid grid-cols-3 gap-2">
                    <UFormField label="Written"><UInput v-model="exWritten" placeholder="8082.1.1" /></UFormField>
                    <UFormField label="Seen"><UInput v-model="exSeen" placeholder="82" /></UFormField>
                    <UFormField label="Era"><UInput v-model="exEra" placeholder="AC" /></UFormField>
                  </div>
                  <UButton
                    :label="workedOut === undefined ? 'Add this era' : `Add ${exEra.trim() || 'this era'}, offset ${workedOut}`"
                    color="primary"
                    variant="solid"
                    class="self-end"
                    :disabled="workedOut === undefined || !exEra.trim()"
                    @click="setEras([...eras, { name: exEra.trim(), offset: workedOut ?? 0 }])"
                  />
                </div>
              </template>
            </UPopover>
          </div>
        </FieldGroup>
      </div>

      <div class="flex flex-col">
        <FieldGroup title="Publishing">
          <template #end>
            <ULink :to="{ name: 'release', params: { id: workspaceId } }" class="text-12 text-secondary">Open Publish</ULink>
          </template>
          <UFormField label="Workshop item">
            <div class="flex gap-2">
              <UInput
                :model-value="listing?.remoteFileId"
                readonly
                placeholder="Not published"
                class="grow"
                :ui="{ base: 'font-mono' }"
              />
              <UButton label="Unlink" variant="ghost" size="md" :disabled="!listing?.remoteFileId" @click="unlink" />
            </div>
          </UFormField>
          <div class="grid grid-cols-2 gap-2.5">
            <UFormField label="Description, Markdown">
              <UInput
                :model-value="mod.descMdRel"
                :placeholder="listing?.descMdRel"
                class="w-full"
                :ui="{ base: 'font-mono' }"
                @change="saveFields({ descMdRel: typed($event) })"
              />
            </UFormField>
            <UFormField label="Description, BBCode">
              <UInput
                :model-value="mod.descBbRel"
                :placeholder="listing?.descBbRel"
                class="w-full"
                :ui="{ base: 'font-mono' }"
                @change="saveFields({ descBbRel: typed($event) })"
              />
            </UFormField>
          </div>
        </FieldGroup>

        <FieldGroup title="Used by and uses">
          <template #end>
            <ULink :to="{ name: 'health', params: { id: workspaceId } }" class="text-12 text-secondary">Open in Health</ULink>
          </template>
          <span v-if="!usage" class="text-12 text-dimmed">Shown once the workspace is open.</span>
          <span v-else-if="!usage.readers.length && !usage.replaced" class="text-12 text-dimmed">
            No other mod reads this one, and it replaces nothing of the game's.
          </span>
          <template v-else>
            <div v-for="row in usage.readers" :key="row.from" class="flex items-center gap-2 text-12 text-toned">
              <OriginChip :label="store.originName(row.from)" :color="store.originColor(row.from)" small />
              reads {{ row.count }} of this mod's objects
            </div>
            <div v-if="usage.replaced" class="flex items-center gap-2 text-12 text-toned">
              <OriginChip
                :label="store.originName(store.originVanilla)"
                :color="store.originColor(store.originVanilla)"
                small
              />
              replaced in {{ usage.replaced }} places by this mod
            </div>
          </template>
        </FieldGroup>

        <FieldGroup
          title="Upload exclusions"
          :hint="upload ? `${upload.left?.length ?? 0} files left out of uploads` : ''"
        >
          <template #end>
            <UModal
              :open="editing"
              title="Upload exclusions"
              description="Gitignore patterns, kept in this workspace. Uploads always leave out dotfiles but .metadata, .gitignore and what the mod's .gitignore names."
              @update:open="edit"
            >
              <UButton label="Edit" />
              <template #body>
                <UTextarea v-model="draft" :rows="10" class="w-full" :ui="{ base: 'font-mono' }" placeholder="gfx/source/**" />
              </template>
              <template #footer>
                <UButton label="Import .workshop-ignore" variant="ghost" @click="importExclusions()" />
                <UButton label="Export .workshop-ignore" variant="ghost" @click="exportExclusions" />
                <span class="grow" />
                <UButton label="Cancel" @click="edit(false)" />
                <UButton label="Save" color="primary" variant="solid" @click="saveExclusions" />
              </template>
            </UModal>
          </template>
          <div v-if="patterns.length" class="rounded-lg border border-default bg-(--bg-0) py-1.5 font-mono text-12">
            <div v-for="(line, i) in patterns" :key="i" class="ps-3 whitespace-pre">{{ line }}</div>
          </div>
          <span v-else class="text-12 text-dimmed">Nothing beyond what every upload leaves out.</span>
        </FieldGroup>
      </div>
    </div>
  </div>
</template>
