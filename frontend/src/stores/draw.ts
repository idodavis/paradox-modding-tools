/**
 * The Visual view's client state, shared by a GUI file's pane and its Widgets panel: the toolbar's choices, kept on
 * this computer, and per file the entry drawn, the part selected and the parts hidden for the session. What is
 * drawn is the query's (queries.ts).
 */
import { computed, ref } from "vue";
import { defineStore } from "pinia";
import { useLocalStorage } from "@vueuse/core";
import type { Request as DrawRequest } from "@services/internal/draw";
import { LOC_LANGUAGES } from "../app/pages";
import { useWorkspaceStore } from "./workspace";

/** The resolutions a window can be laid out at, each by its width. */
export const RESOLUTIONS = [
  { label: "1280 by 720", value: 1280, height: 720 },
  { label: "1600 by 900", value: 1600, height: 900 },
  { label: "1920 by 1080", value: 1920, height: 1080 },
  { label: "2560 by 1440", value: 2560, height: 1440 },
  { label: "3840 by 2160", value: 3840, height: 2160 },
];

/** The UI scales a window can be laid out at. */
export const SCALES = [
  { label: "100%", value: 1 },
  { label: "125%", value: 1.25 },
  { label: "150%", value: 1.5 },
  { label: "200%", value: 2 },
];

/** Holds the toolbar's choices, and per file the entry picked and the part selected. */
export const useDrawStore = defineStore("draw", () => {
  const workspace = useWorkspaceStore();

  const width = useLocalStorage("pmt.draw.width", 1920);
  const height = computed(() => RESOLUTIONS.find((one) => one.value === width.value)?.height ?? 1080);
  const scale = useLocalStorage("pmt.draw.scale", 1);
  const bounds = useLocalStorage("pmt.draw.bounds", false);

  // the workspace's language first; the one chosen is kept, else it is the one shown
  const defaultLanguage = computed(() => workspace.activeWorkspace?.defaultLocLang || "english");
  const languages = computed(() => [
    ...LOC_LANGUAGES.filter((one) => one.value === defaultLanguage.value),
    ...LOC_LANGUAGES.filter((one) => one.value !== defaultLanguage.value),
  ]);
  const chosenLanguage = useLocalStorage("pmt.draw.language", "");
  const language = computed({
    get: () => chosenLanguage.value || defaultLanguage.value,
    set: (value: string) => (chosenLanguage.value = value),
  });

  // by file path: the entry the user picked, the part selected, and the parts hidden for this session
  const picked = ref(new Map<string, string>());
  const selected = ref(new Map<string, number>());
  const hidden = ref(new Map<string, Set<number>>());

  /** Hides a file's part, or shows it again; what it holds goes with it. Kept until the app closes. */
  function toggleHidden(path: string, part: number): void {
    const parts = hidden.value.get(path) ?? new Set<number>();
    if (!parts.delete(part)) parts.add(part);
    hidden.value.set(path, parts);
  }

  /** What a file's entry is drawn as: the toolbar's resolution, scale and language. */
  function requestOf(path: string, entry: string): DrawRequest {
    return {
      path,
      name: entry,
      width: width.value,
      height: height.value,
      scale: scale.value,
      language: language.value,
    };
  }

  return { width, height, scale, bounds, language, languages, picked, selected, hidden, toggleHidden, requestOf };
});
