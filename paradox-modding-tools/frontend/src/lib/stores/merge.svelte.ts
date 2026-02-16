import { getContext, setContext } from "svelte";
import { GetGameScriptRoot } from "@services/fileservice";
import { MergePreview, MergePairs, MergeDirs, WriteMergedFile, GetMergeConflicts } from "@services/mergeservice";
import type { FileMergeResult, MergerOptions } from "@services/models";
import { appSettings, saveSettings, game, gameInstallPath } from "@stores/app.svelte";
import { get } from "svelte/store";


export type PreviewItem = {
  relPath: string;
  pathA: string;
  pathB: string;
  wouldOverwrite: boolean;
  outputPath?: string;
};

const MERGE_STORE_KEY = Symbol("MERGE_STORE");

export class MergeStore {
  // Configuration
  config = $state({
    addAdditionalEntries: true,
    manualConflictResolution: false,
    entryPlacement: "bottom",
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
  
  // UI State
  selectedForDiff = $state<{ pathA: string; pathB: string } | null>(null);
  
  // Manual Merge State
  manualMergeQueue = $state<{ relPath: string; pathA: string; pathB: string }[]>([]);
  currentManualFile = $state<{ relPath: string; pathA: string; pathB: string } | null>(null);
  manualRoots = $state<{ a: string; b: string }>({ a: "", b: "" });

  mergePromise: (Promise<unknown> & { cancel?: () => void }) | null = null;

  addPair(pathA = "", pathB = "", outputName = "") {
    this.filePairs.push({ pathA, pathB, outputName });
  }

  removePair(i: number) {
    this.filePairs.splice(i, 1);
  }

  updatePair(
    i: number,
    field: "pathA" | "pathB" | "outputName",
    value: string,
  ) {
    this.filePairs[i][field] = value;
  }

  constructor() {
    // Initialize output dir from settings
    const def = get(appSettings)?.mergeOutputDir;
    if (def) this.outputDir = def;
  }

  get options(): MergerOptions {
    return {
      addAdditionalEntries: this.config.addAdditionalEntries,
      manualConflictResolution: this.config.manualConflictResolution,
      entryPlacement: this.config.entryPlacement,
      keyList: this.config.useKeyList
        ? this.config.customKeys.split(/\r?\n/).map((s) => s.trim()).filter(Boolean)
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

  get labels() {
    if (this.activeTab === "vanilla") return { a: "Vanilla", b: "Mod" };
    if (this.activeTab === "dirs") return { a: "Set A", b: "Set B" };
    return { a: "File A", b: "File B" };
  }

  reset() {
    this.mergeResults = [];
    this.selectedForDiff = null;
    this.errorMsg = "";
    this.previewItems = [];
    this.selectedRelPaths = {};
  }

  async runPreview(mode: "vanilla" | "dirs") {
    if (!this.canRun[mode]) return;
    this.previewing = true;
    this.previewItems = [];
    this.selectedRelPaths = {};
    this.errorMsg = "";
    
    try {
      const installPath = get(gameInstallPath) ?? "";
      const currentGame = get(game);
      
      const setAPath = mode === "vanilla" 
        ? await GetGameScriptRoot(currentGame, installPath.trim()) 
        : this.pathA;
      const setBPath = mode === "vanilla" ? this.modPath : this.pathB;

      const items = await MergePreview(setAPath, setBPath, this.outputDir, this.options) ?? [];
      this.previewItems = items;
      
      const sel: Record<string, boolean> = {};
      for (const p of items) sel[p.relPath] = true;
      this.selectedRelPaths = sel;
      
      if (!items.length) this.errorMsg = "No matching files found.";
    } catch (e) {
      this.errorMsg = e instanceof Error ? e.message : String(e);
    } finally {
      this.previewing = false;
    }
  }

  async runMerge(mode: "vanilla" | "dirs") {
    if (!this.canRun[mode]) return;
    this.merging = true;
    this.mergeResults = [];
    this.errorMsg = "";
    
    try {
      const installPath = get(gameInstallPath) ?? "";
      const currentGame = get(game);
      
      const setAPath = mode === "vanilla" 
        ? await GetGameScriptRoot(currentGame, installPath.trim()) 
        : this.pathA;
      const setBPath = mode === "vanilla" ? this.modPath : this.pathB;

      if (this.config.manualConflictResolution) {
        const toProcess = this.previewItems.filter((p) => this.selectedRelPaths[p.relPath] !== false);
        this.manualMergeQueue = toProcess.map((p) => ({
          relPath: p.relPath,
          pathA: p.pathA,
          pathB: p.pathB,
        }));
        this.manualRoots = { a: setAPath, b: setBPath };
        this.processManualQueue();
        return;
      }

      const selectedForMerge = this.previewItems
        .filter((p) => this.selectedRelPaths[p.relPath] !== false)
        .map((p) => p.relPath);

      if (selectedForMerge.length > 0) {
        this.mergePromise = MergeDirs(
          setAPath, setBPath, this.outputDir, this.options, selectedForMerge
        );
        const res = await this.mergePromise;
        this.mergeResults = (res as FileMergeResult[]) ?? [];
      }
      
      if (this.mergeResults.length === 0 && selectedForMerge.length === 0) {
        this.errorMsg = "No files selected.";
      }
      this.saveOutputDir();
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      if (!msg.toLowerCase().includes("cancel")) this.errorMsg = msg;
    } finally {
      this.merging = false;
      this.mergePromise = null;
    }
  }

  async runPairMerge() {
    if (!this.canRun.pairs) return;
    this.merging = true;
    this.mergeResults = [];
    this.errorMsg = "";
    
    try {
      if (this.filePairs.length > 0) {
        this.mergePromise = MergePairs(this.filePairs, this.outputDir, this.options);
        const res = await this.mergePromise;
        this.mergeResults = (res as FileMergeResult[]) ?? [];
      }
      this.saveOutputDir();
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      if (!msg.toLowerCase().includes("cancel")) this.errorMsg = msg;
    } finally {
      this.merging = false;
      this.mergePromise = null;
    }
  }

  async processManualQueue() {
    if (this.manualMergeQueue.length === 0) {
      this.merging = false;
      return;
    }
    const next = this.manualMergeQueue[0];
    try {
      const conflicts = await GetMergeConflicts(next.pathA, next.pathB, this.options);
      if (conflicts.some((c: any) => c.type === "conflict")) {
        this.currentManualFile = next;
      } else {
        const res = await MergeDirs(
          this.manualRoots.a, this.manualRoots.b, this.outputDir, this.options, [next.relPath]
        );
        if (res && res.length > 0) this.mergeResults.push(...res);
        this.manualMergeQueue.shift();
        this.processManualQueue();
      }
    } catch (e) {
      console.error("Error checking conflicts for", next.relPath, e);
      this.manualMergeQueue.shift();
      this.processManualQueue();
    }
  }

  async onManualSave(content: string, stats: { changed: number; added: number; removed: number }) {
    if (!this.currentManualFile) return;
    const item = this.previewItems.find((p) => p.relPath === this.currentManualFile!.relPath);
    const outPath = item?.outputPath || `${this.outputDir}/${this.currentManualFile.relPath}`;
    
    try {
      await WriteMergedFile(outPath, content);
      this.mergeResults.push({
        filePath: this.currentManualFile.relPath,
        fileAPath: this.currentManualFile.pathA,
        fileBPath: this.currentManualFile.pathB,
        outputPath: outPath,
        changed: stats.changed, added: stats.added, removed: stats.removed,
        resolvedConflicts: [{ key: "Manual", usedSide: "Manual", reason: "User selection" }],
      });
    } catch (e) {
      this.errorMsg = "Failed to save manual merge: " + e;
    }
    this.onManualSkip();
  }

  onManualSkip() {
    this.currentManualFile = null;
    this.manualMergeQueue.shift();
    this.processManualQueue();
  }

  cancelMerge() {
    if (this.mergePromise?.cancel) this.mergePromise.cancel();
    this.mergePromise = null;
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
