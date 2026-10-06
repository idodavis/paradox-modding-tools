/**
 * Guide toast snooze and shared column-open / IDE-visible flags.
 */
import { computed, shallowRef } from "vue";
import { useSettingsStore } from "../stores/settings";

type GuidePref = "show" | "1h" | "restart" | "off";

const PREFS: GuidePref[] = ["show", "1h", "restart", "off"];

/** Select items for Display popover duration fields. */
export const GUIDE_PREF_ITEMS = [
  { label: "Show", value: "show" },
  { label: "Hide for 1 hour", value: "1h" },
  { label: "Hide until restart", value: "restart" },
  { label: "Hide forever", value: "off" },
] as const;

/** User wants the Guide column open. */
export const guideOpen = shallowRef(false);
/** IDE workbench page is the visible tool. */
export const ideVisible = shallowRef(false);

/** Open or close the Guide column. */
export function setGuideOpen(open: boolean): void {
  guideOpen.value = open;
}

function parsePref(raw: string | undefined): GuidePref {
  return PREFS.includes(raw as GuidePref) ? (raw as GuidePref) : "show";
}

function allowed(pref: GuidePref): boolean {
  switch (pref) {
    case "off":
      return false;
    case "restart":
      return sessionStorage.getItem("pmt.guide.toast.restart") !== "1";
    case "1h": {
      const until = Number(sessionStorage.getItem("pmt.guide.toast.until") || 0);
      return !until || Date.now() >= until;
    }
    case "show":
      return true;
    default: {
      const _never: never = pref;
      return _never;
    }
  }
}

/** Read and write Guide toast visibility prefs. */
export function useGuidePrefs() {
  const settings = useSettingsStore();
  const toastPref = computed(() => parsePref(settings.values["ui.guide.toast"]));

  /** Persist a duration and stamp sessionStorage for 1h / restart. */
  async function setToastPref(value: GuidePref): Promise<void> {
    if (value === "1h") {
      sessionStorage.setItem(
        "pmt.guide.toast.until",
        String(Date.now() + 60 * 60 * 1000),
      );
    } else {
      sessionStorage.removeItem("pmt.guide.toast.until");
    }
    if (value === "restart") {
      sessionStorage.setItem("pmt.guide.toast.restart", "1");
    } else {
      sessionStorage.removeItem("pmt.guide.toast.restart");
    }
    await settings.set("ui.guide.toast", value);
  }

  return {
    toastPref,
    toastAllowed: () => allowed(toastPref.value),
    setToastPref,
  };
}
