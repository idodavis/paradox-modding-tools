import { getContext, setContext } from "svelte";
import { game } from "@stores/app.svelte";
import { get } from "svelte/store";
import { InventorySummary, InventoryItemRow } from "@services/models";
import {
  GetSupportedTypes,
  ListInventoriesForGame,
  ExtractInventory,
  GetInventoryItems,
  SaveInventory,
  RenameInventory,
  DeleteInventory,
} from "@services/inventoryservice";

const INVENTORY_STORE_KEY = Symbol("INVENTORY_STORE");

export class InventoryStore {
  // State
  file = $state<string>("");
  selectedTypes = $state<string[]>([]);
  supportedTypes = $state<string[]>([]);

  hasExtraction = $state(false);
  loading = $state(false);
  extractionErrors = $state<string[]>([]);

  allItems = $state<InventoryItemRow[]>([]);
  currentInventoryId = $state<string | null>(null);
  currentInventoryGame = $state<string | null>(null);

  selectedRow = $state<InventoryItemRow | null>(null);
  savedInventories = $state<InventorySummary[]>([]);
  isCurrentTemp = $state(false);

  itemDetailsOpen = $state(false);

  nameModal = $state<{
    open: boolean;
    mode: "save" | "rename";
    invId: string | null;
  }>({
    open: false,
    mode: "save",
    invId: null,
  });

  extractionPromise: (Promise<unknown> & { cancel?: () => void }) | null = null;

  get typesDisabled() {
    return this.supportedTypes.length === 0;
  }

  get extractDisabled() {
    return !this.file || !this.selectedTypes.length;
  }

  get nameModalInitialName() {
    if (this.nameModal.mode === "save") {
      const g = get(game);
      return `${g} - ${new Date().toISOString().slice(0, 10)} - ${Math.random().toString(36).slice(2, 8)}`;
    }
    return this.savedInventories.find((i) => i.id === this.nameModal.invId)?.name ?? "";
  }

  async refresh() {
    const g = get(game);
    const [types, list] = await Promise.all([GetSupportedTypes(g), ListInventoriesForGame(g)]);
    this.supportedTypes = types ?? [];
    this.savedInventories = list ?? [];

    // Clear if game changed and we have an active inventory from another game
    if (this.currentInventoryId && this.currentInventoryGame !== null && this.currentInventoryGame !== g) {
      this.clearAll();
    }
  }

  async doExtract() {
    if (this.extractDisabled) return;
    this.loading = true;
    this.clearAll();
    const g = get(game);

    try {
      // Unwrap proxies to ensure Wails can marshal them correctly
      const filesArg = $state.snapshot(this.file);
      const typesArg = $state.snapshot(this.selectedTypes);

      this.extractionPromise = ExtractInventory(g, filesArg, typesArg);
      const inventoryId = (await this.extractionPromise) as string | null;
      if (inventoryId) {
        this.currentInventoryId = inventoryId;
        this.currentInventoryGame = g;
        this.hasExtraction = true;
        this.isCurrentTemp = true;
        this.allItems = (await GetInventoryItems(inventoryId)) ?? [];
      }
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      if (!msg.toLowerCase().includes("cancel")) this.extractionErrors = [msg];
    } finally {
      this.loading = false;
      this.extractionPromise = null;
    }
  }

  async loadInventory(inv: InventorySummary) {
    this.currentInventoryId = inv.id;
    this.currentInventoryGame = inv.game;
    this.hasExtraction = true;
    this.isCurrentTemp = false;
    this.allItems = (await GetInventoryItems(inv.id)) ?? [];
  }

  async handleNameModalSave(name: string) {
    if (!this.nameModal.invId) return;
    if (this.nameModal.mode === "save") {
      await SaveInventory(this.nameModal.invId, name);
      this.isCurrentTemp = false;
    } else {
      await RenameInventory(this.nameModal.invId, name);
    }
    await this.refresh();
  }

  async handleDelete(inv: InventorySummary) {
    await DeleteInventory(inv.id);
    if (this.currentInventoryId === inv.id) this.clearAll();
    this.refresh();
  }

  clearAll() {
    this.hasExtraction = false;
    this.extractionErrors = [];
    this.allItems = [];
    this.currentInventoryId = null;
    this.currentInventoryGame = null;
    this.isCurrentTemp = false;
    this.selectedRow = null;
    this.itemDetailsOpen = false;
  }

  openModal(mode: "save" | "rename", inv?: InventorySummary) {
    this.nameModal = {
      open: true,
      mode,
      invId: this.currentInventoryId ?? inv?.id ?? null,
    };
  }

  cancelExtraction() {
    if (this.extractionPromise?.cancel) {
      this.extractionPromise.cancel();
    }
    this.extractionPromise = null;
    this.loading = false;
  }
}

export const inventoryStore = new InventoryStore();

export function createInventoryStore() {
  return new InventoryStore();
}

export function setInventoryStore(store: InventoryStore) {
  setContext(INVENTORY_STORE_KEY, store);
}

export function getInventoryStore() {
  return getContext<InventoryStore>(INVENTORY_STORE_KEY);
}
