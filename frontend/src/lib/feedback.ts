/** The ways any part of the app talks to the user outside a page: confirmations, toasts and copying. */
import { Clipboard } from "@wailsio/runtime";

import { Error as logError } from "../../bindings/github.com/wailsapp/wails/v3/pkg/services/log/logservice";
import ConfirmModal from "../ui/ConfirmModal.vue";
import { splitError } from "./format";

const TOAST_LINES = 3;

/** confirm is the app's one confirmation dialog. Its `open` resolves with the user's choice. */
export const confirm = useOverlay().create(ConfirmModal);

/** toast is the app's toasts. It is undefined until takeToasts runs. */
export let toast: ReturnType<typeof useToast> | undefined;

/** takeToasts stores the app's toasts in `toast`. Nuxt UI hands them out only inside the app's context. */
export function takeToasts(): void {
  toast = useToast();
}

/** writeLog writes an error to pmt.log, tagged with the given source. */
export function writeLog(error: unknown, source: string): void {
  const failure = error instanceof Error ? error : new Error(String(error));
  void logError(failure.message, "source", source, "stack", failure.stack ?? "");
}

/** showError shows an error as a toast. */
export function showError(error: unknown): void {
  const [summary, details] = splitError(error, TOAST_LINES);
  toast?.add({ title: summary, description: details, color: "error", ui: { description: "whitespace-pre-wrap" } });
}

/**
 * isCancelled reports whether an error is a cancel, which is neither shown
 * nor logged: the user cancelling a system dialog, or Monaco's "Canceled".
 */
export function isCancelled(error: unknown): boolean {
  const failure = error as { name?: unknown; message?: unknown } | null | undefined;

  return failure?.name === "Canceled" || String(failure?.message).includes("cancelled by user");
}

/** reportError shows an error and logs it, unless it is a cancel. */
export function reportError(error: unknown, source: string): void {
  if (isCancelled(error)) return;
  showError(error);
  writeLog(error, source);
}

/** copyText puts a text on the clipboard and shows a toast saying what was copied. */
export async function copyText(text: string, what: string, note = ""): Promise<void> {
  await Clipboard.SetText(text);
  toast?.add({ title: `${what} copied`, description: note, color: "success" });
}
