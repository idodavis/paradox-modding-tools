/**
 * The Wiki's suggestion toast (ui.wiki.toast): shown, or hidden for an hour, until a restart, or for good.
 */
import { computed } from "vue";
import { useSettingsStore } from "../stores/settings";

type ToastPref = "show" | "1h" | "restart" | "off";

/** The toast's choices, as the IDE settings card lists them. */
export const TOAST_PREFS: { label: string; value: ToastPref }[] = [
  { label: "Show", value: "show" },
  { label: "Hide for 1 hour", value: "1h" },
  { label: "Hide until restart", value: "restart" },
  { label: "Hide forever", value: "off" },
];

const isPref = (raw: string | undefined): raw is ToastPref => TOAST_PREFS.some((pref) => pref.value === raw);

export function useWikiPrefs() {
  const settings = useSettingsStore();
  const toastPref = computed<ToastPref>(() => {
    const stored = settings.values["ui.wiki.toast"];
    return isPref(stored) ? stored : "show";
  });

  /** Whether the toast may show now: an hour's or a restart's hiding is kept in this run's session storage. */
  function toastAllowed(): boolean {
    switch (toastPref.value) {
      case "off":
        return false;
      case "restart":
        return sessionStorage.getItem("pmt.wiki.toast.restart") !== "1";
      case "1h":
        return Date.now() >= Number(sessionStorage.getItem("pmt.wiki.toast.until") || 0);
      case "show":
        return true;
    }
  }

  function setToastPref(value: ToastPref): void {
    if (value === "1h") sessionStorage.setItem("pmt.wiki.toast.until", String(Date.now() + 60 * 60 * 1000));
    else sessionStorage.removeItem("pmt.wiki.toast.until");
    if (value === "restart") sessionStorage.setItem("pmt.wiki.toast.restart", "1");
    else sessionStorage.removeItem("pmt.wiki.toast.restart");
    settings.set("ui.wiki.toast", value);
  }

  return { toastPref, toastAllowed, setToastPref };
}
