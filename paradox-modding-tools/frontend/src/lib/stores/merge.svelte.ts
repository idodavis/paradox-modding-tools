import { getContext, setContext } from "svelte";
import { GetGameScriptRoot, WriteWithBOM } from "@services/fileservice";
import { MergePreview, Merge, GetMergeConflicts } from "@services/mergeservice";
import type { FileMergeResult, MergerOptions, MergeConflictChunk, PreviewItem } from "@services/models";
import { appSettings, saveSettings, game, gameInstallPath } from "@stores/app.svelte";
import { get } from "svelte/store";

const isCancelError = (e: unknown) => String(e).toLowerCase().includes("cancel");

export function pairsToTasks(
  pairs: { pathA: string; pathB: string; outputName: string }[],
  outputDir: string,
): PreviewItem[] {
  const base = outputDir.replace(/\/?$/, "");
  return pairs
    .filter((p) => p.pathA && p.pathB)
    .map((p) => {
      const baseName = p.outputName?.trim() || p.pathA.split(/[/\\]/).pop() || "output";
      const relPath = baseName.replace(/\.[^.]+$/, "") + ".txt";
      return { relPath, pathA: p.pathA, pathB: p.pathB, outputPath: `${base}/${relPath}`, wouldOverwrite: false };
    });
}

const MERGE_STORE_KEY = Symbol("MERGE_STORE");

export class MergeStore {
  // Configuration
  config = $state({
    addAdditionalEntries: true,
    manualConflictResolution: false,
    useKeyList: false,
    customKeys: "",
    matchByFilenameOnly: false,
    includePathPattern: "",
    excludePathPattern: "",
    outputFileSuffix: "",
  });

  // Inputs
  pathA = $state<string>("");
  pathB = $state<string>("");
  modPath = $state<string>("");
  filePairs = $state<{ pathA: string; pathB: string; outputName: string }[]>([]);

  // Output
  outputDir = $state("");
  rememberOutputDir = $state(false);

  // State
  activeTab = $state("vanilla");
  merging = $state(false);
  previewing = $state(false);
  errorMsg = $state("");

  // Data
  previewItems = $state<PreviewItem[]>([]);
  selectedRelPaths = $state<Record<string, boolean>>({});
  mergeResults = $state<FileMergeResult[]>([]);

  manualMergeQueue = $state<PreviewItem[]>([]);
  currentManualFile = $state<{ task: PreviewItem; chunks: MergeConflictChunk[] } | null>(null);

  /** Persists across the manual-merge queue so layout sticks after save/skip */
  mergeResultLayout = $state<"right" | "bottom">("right");

  mergePromise: (Promise<unknown> & { cancel?: () => void }) | null = null;

  addPair(pathA = "", pathB = "", outputName = "") {
    this.filePairs.push({ pathA, pathB, outputName });
  }

  removePair(i: number) {
    this.filePairs.splice(i, 1);
  }

  updatePair(i: number, field: "pathA" | "pathB" | "outputName", value: string) {
    this.filePairs[i][field] = value;
  }

  constructor() {
    // Initialize output dir from settings
    const def = get(appSettings)["_global.merge_output_dir"];
    if (def) this.outputDir = def;
  }

  get options(): MergerOptions {
    return {
      addAdditionalEntries: this.config.addAdditionalEntries,
      manualConflictResolution: this.config.manualConflictResolution,
      keyList: this.config.useKeyList
        ? this.config.customKeys
            .split(/\r?\n/)
            .map((s) => s.trim())
            .filter(Boolean)
        : [],
      matchByFilenameOnly: this.config.matchByFilenameOnly,
      includePathPattern: this.config.includePathPattern,
      excludePathPattern: this.config.excludePathPattern,
      outputFileSuffix: this.config.outputFileSuffix,
    };
  }

  get canRun() {
    const installPath = get(gameInstallPath);
    return {
      vanilla: !!installPath?.trim() && !!this.modPath?.trim() && !!this.outputDir,
      dirs: !!this.pathA?.trim() && !!this.pathB?.trim() && !!this.outputDir,
      pairs: this.filePairs.length > 0 && this.filePairs.some((p) => p.pathA && p.pathB) && !!this.outputDir,
    };
  }

  private static LABELS = {
    vanilla: { a: "Vanilla", b: "Mod" },
    dirs: { a: "Dir A", b: "Dir B" },
    pairs: { a: "File A", b: "File B" },
  } as const;
  get labels() {
    return MergeStore.LABELS[this.activeTab as keyof typeof MergeStore.LABELS] ?? MergeStore.LABELS.pairs;
  }

  reset() {
    this.mergeResults = [];
    this.errorMsg = "";
    this.previewItems = [];
    this.selectedRelPaths = {};
  }

  private async getPathsForMode(mode: "vanilla" | "dirs") {
    const installPath = get(gameInstallPath) ?? "";
    const currentGame = get(game);
    const pathA = mode === "vanilla" ? await GetGameScriptRoot(currentGame, installPath.trim()) : this.pathA;
    const pathB = mode === "vanilla" ? this.modPath : this.pathB;
    return { pathA, pathB };
  }

  async runPreview(mode: "vanilla" | "dirs") {
    if (!this.canRun[mode]) return;
    this.previewing = true;
    this.previewItems = [];
    this.selectedRelPaths = {};
    this.errorMsg = "";
    try {
      const { pathA, pathB } = await this.getPathsForMode(mode);
      const items = (await MergePreview(pathA, pathB, this.outputDir, this.options)) ?? [];
      this.previewItems = items;
      this.selectedRelPaths = Object.fromEntries(items.map((p) => [p.relPath, true]));
      if (!items.length) this.errorMsg = "No matching files found.";
    } catch (e) {
      this.errorMsg = e instanceof Error ? e.message : String(e);
    } finally {
      this.previewing = false;
    }
  }

  private async runAsyncMerge(fn: () => Promise<FileMergeResult[]>) {
    this.merging = true;
    this.mergeResults = [];
    this.errorMsg = "";
    try {
      const res = await fn();
      this.mergeResults = Array.isArray(res) ? res : [];
      this.saveOutputDir();
    } catch (e) {
      if (!isCancelError(e)) this.errorMsg = e instanceof Error ? e.message : String(e);
    } finally {
      this.merging = false;
      this.mergePromise = null;
    }
  }

  async runMergeWithTasks(tasks: PreviewItem[]) {
    if (!tasks.length) {
      this.errorMsg = "No files selected.";
      return;
    }
    if (this.config.manualConflictResolution) {
      this.mergeResults = [];
      this.errorMsg = "";
      this.manualMergeQueue = tasks;
      this.merging = true;
      this.processManualQueue();
      return;
    }
    this.mergePromise = Merge(tasks, this.options);
    await this.runAsyncMerge(() => this.mergePromise! as Promise<FileMergeResult[]>);
  }

  async runDirMerge(mode: "vanilla" | "dirs") {
    if (!this.canRun[mode]) return;
    const selected = this.previewItems.filter((p) => this.selectedRelPaths[p.relPath] !== false);
    await this.runMergeWithTasks(selected);
  }

  async runPairMerge() {
    if (!this.canRun.pairs) return;
    const tasks = pairsToTasks(this.filePairs, this.outputDir);
    if (!tasks.length) return;
    await this.runMergeWithTasks(tasks);
  }

  async processManualQueue() {
    if (this.manualMergeQueue.length === 0) {
      this.merging = false;
      return;
    }
    const task = this.manualMergeQueue[0];
    try {
      const chunks = await GetMergeConflicts(task.pathA, task.pathB, this.options);
      this.currentManualFile = { task, chunks };
    } catch (e) {
      console.error("Error checking conflicts for", task.relPath, e);
      this.manualMergeQueue.shift();
      this.processManualQueue();
    }
  }

  async autoMergeCurrentFile() {
    if (!this.currentManualFile) return;
    const { task } = this.currentManualFile;
    try {
      const results = await Merge([task], this.options);
      if (results?.length) this.mergeResults.push(...results);
    } catch (e) {
      this.errorMsg = `Failed to auto-merge ${task.relPath}: ${e}`;
    }
    this.manualNext();
  }

  async manualSave(content: string, stats: { changed: number; added: number }) {
    if (!this.currentManualFile) return;
    const { task } = this.currentManualFile;
    try {
      await WriteWithBOM(task.outputPath, content);
      this.mergeResults.push({
        filePath: task.relPath,
        fileAPath: task.pathA,
        fileBPath: task.pathB,
        outputPath: task.outputPath,
        ...stats,
        resolvedConflicts: [{ key: "Manual", usedSide: "Manual", reason: "User selection" }],
      });
    } catch (e) {
      this.errorMsg = "Failed to save manual merge: " + e;
    }
    this.manualNext();
  }

  manualNext() {
    this.currentManualFile = null;
    this.manualMergeQueue.shift();
    this.processManualQueue();
  }

  cancelMerge() {
    if (this.mergePromise?.cancel) this.mergePromise.cancel();
    this.mergePromise = null;
  }

  cancelManualMerge() {
    this.currentManualFile = null;
    this.manualMergeQueue = [];
    this.merging = false;
  }

  private saveOutputDir() {
    if (this.rememberOutputDir && this.outputDir) {
      appSettings.update((s) => ({ ...s, "_global.merge_output_dir": this.outputDir }));
      saveSettings();
    }
  }
}

export function setMergeStore(store?: MergeStore): MergeStore {
  const s = store ?? new MergeStore();
  setContext(MERGE_STORE_KEY, s);
  return s;
}

export function getMergeStore() {
  return getContext<MergeStore>(MERGE_STORE_KEY);
}
