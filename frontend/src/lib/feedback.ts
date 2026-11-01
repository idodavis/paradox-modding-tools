/** The ways any part of the app talks to the user outside a page: the confirmation dialog, the toasts, and a copy. */
import { Clipboard } from "@wailsio/runtime";

import ConfirmModal from "../ui/ConfirmModal.vue";

/** confirm is the app's one confirmation dialog. Its `open` resolves with the choice the user made. */
export const confirm = useOverlay().create(ConfirmModal);

/** toast is the app's toasts once the app has taken them, and undefined before. */
export let toast: ReturnType<typeof useToast> | undefined;

/**
 * takeToasts keeps the app's toasts for all of the app. Nuxt UI hands them out only in the app's context, such as a
 * component's setup.
 */
export function takeToasts(): void {
  toast = useToast();
}

/** copyText puts a text on the clipboard and says so in a toast: "<what> copied", with the given note under it. */
export async function copyText(text: string, what: string, note = ""): Promise<void> {
  await Clipboard.SetText(text);
  toast?.add({ title: `${what} copied`, description: note, color: "success" });
}
