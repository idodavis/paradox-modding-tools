import { getContext, setContext } from "svelte";
import { GetGameScriptRoot } from "@services/fileservice";
import { MergePreview, MergePairs, MergeDirs, WriteMergedFile, GetMergeConflicts } from "@services/mergeservice";
import type { FileMergeResult, MergerOptions, MergeConflictChunk, PreviewItem } from "@services/models";
import { appSettings, saveSettings, game, gameInstallPath } from "@stores/app.svelte";
import { get } from "svelte/store";

type ResolvedSide = "A" | "B" | "Custom" | undefined;

export const getConflictIndices = (chunks: MergeConflictChunk[]) =>
  chunks.flatMap((c, i) => (c.type === "conflict" ? [i] : []));

export const buildMergedContent = (chunks: MergeConflictChunk[], rv: Record<number, string>) =>
  chunks.map((c, i) => (c.type === "unchanged" ? c.textA : c.type === "added" ? c.textB : (rv[i] ?? ""))).join("");

export const computeMergeStats = (chunks: MergeConflictChunk[], resolved: Record<number, ResolvedSide>) => ({
  changed: chunks.filter((c, i) => c.type === "conflict" && resolved[i] !== "A").length,
  added: chunks.filter((c) => c.type === "added").length,
});

const isCancelError = (e: unknown) => String(e).toLowerCase().includes("cancel");

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
    outputFilename: "",
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

  manualMergeQueue = $state<{ relPath: string; pathA: string; pathB: string }[]>([]);
  currentManualFile = $state<{
    relPath: string;
    pathA: string;
    pathB: string;
    chunks: MergeConflictChunk[];
  } | null>(null);

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
      outputFilename: this.config.outputFilename,
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

  async runMerge(mode: "vanilla" | "dirs") {
    if (!this.canRun[mode]) return;
    const { pathA, pathB } = await this.getPathsForMode(mode);
    const selected = this.previewItems.filter((p) => this.selectedRelPaths[p.relPath] !== false);
    if (this.config.manualConflictResolution) {
      this.mergeResults = [];
      this.errorMsg = "";
      this.manualMergeQueue = selected.map((p) => ({ relPath: p.relPath, pathA: p.pathA, pathB: p.pathB }));
      this.merging = true;
      this.processManualQueue();
      return;
    }
    const relPaths = selected.map((p) => p.relPath);
    if (!relPaths.length) {
      this.errorMsg = "No files selected.";
      return;
    }
    this.mergePromise = MergeDirs(pathA, pathB, this.outputDir, this.options, relPaths);
    await this.runAsyncMerge(() => this.mergePromise! as Promise<FileMergeResult[]>);
  }

  async runPairMerge() {
    if (!this.canRun.pairs) return;
    const validPairs = this.filePairs.filter((p) => p.pathA && p.pathB);
    if (this.config.manualConflictResolution && validPairs.length) {
      this.mergeResults = [];
      this.errorMsg = "";
      const base = this.outputDir.replace(/\/?$/, "");
      const items = validPairs.map((p) => {
        const baseName = p.outputName?.trim() || p.pathA.split(/[/\\]/).pop() || "output";
        const relPath = baseName.replace(/\.[^.]+$/, "") + ".txt";
        return { relPath, pathA: p.pathA, pathB: p.pathB };
      });
      this.previewItems = items.map((p) => ({ ...p, outputPath: `${base}/${p.relPath}`, wouldOverwrite: false }));
      this.selectedRelPaths = Object.fromEntries(items.map((p) => [p.relPath, true]));
      this.manualMergeQueue = items;
      this.merging = true;
      this.processManualQueue();
      return;
    }
    if (!validPairs.length) return;
    this.mergePromise = MergePairs(validPairs, this.outputDir, this.options);
    await this.runAsyncMerge(() => this.mergePromise! as Promise<FileMergeResult[]>);
  }

  async processManualQueue() {
    if (this.manualMergeQueue.length === 0) {
      this.merging = false;
      return;
    }
    const next = this.manualMergeQueue[0];
    try {
      const chunks = await GetMergeConflicts(next.pathA, next.pathB, this.options);
      this.currentManualFile = { relPath: next.relPath, pathA: next.pathA, pathB: next.pathB, chunks };
    } catch (e) {
      console.error("Error checking conflicts for", next.relPath, e);
      this.manualMergeQueue.shift();
      this.processManualQueue();
    }
  }

  async autoMergeCurrentFile() {
    if (!this.currentManualFile) return;
    const { relPath, pathA, pathB } = this.currentManualFile;
    const pair = [{ pathA, pathB, outputName: relPath }];
    try {
      const results = await MergePairs(pair, this.outputDir, this.options);
      if (results?.length) this.mergeResults.push(...results);
    } catch (e) {
      this.errorMsg = `Failed to auto-merge ${relPath}: ${e}`;
    }
    this.manualNext();
  }

  async manualSave(content: string, stats: { changed: number; added: number }) {
    if (!this.currentManualFile) return;
    const { relPath, pathA, pathB } = this.currentManualFile;
    const outPath =
      this.previewItems.find((p) => p.relPath === relPath)?.outputPath ??
      `${this.outputDir.replace(/\/?$/, "")}/${relPath}`;
    try {
      await WriteMergedFile(outPath, content);
      this.mergeResults.push({
        filePath: relPath,
        fileAPath: pathA,
        fileBPath: pathB,
        outputPath: outPath,
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
      appSettings.update((s) => ({ ...s, mergeOutputDir: this.outputDir }));
      saveSettings();
    }
  }
}

export function createMergeStore() {
  return new MergeStore();
}

export function setMergeStore(store: MergeStore) {
  setContext(MERGE_STORE_KEY, store);
}

export function getMergeStore() {
  return getContext<MergeStore>(MERGE_STORE_KEY);
}
