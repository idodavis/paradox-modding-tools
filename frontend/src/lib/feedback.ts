/** The ways any part of the app talks to the user outside a page: confirmations, toasts and copying. */
import { Clipboard } from "@wailsio/runtime";

import ConfirmModal from "../ui/ConfirmModal.vue";

/** confirm is the app's one confirmation dialog. Its `open` resolves with the user's choice. */
export const confirm = useOverlay().create(ConfirmModal);

/** toast is the app's toasts. It is undefined until takeToasts runs. */
export let toast: ReturnType<typeof useToast> | undefined;

/** takeToasts stores the app's toasts in `toast`. Nuxt UI hands them out only inside the app's context. */
export function takeToasts(): void {
  toast = useToast();
}

/** copyText puts a text on the clipboard and shows a toast saying what was copied. */
export async function copyText(text: string, what: string, note = ""): Promise<void> {
  await Clipboard.SetText(text);
  toast?.add({ title: `${what} copied`, description: note, color: "success" });
}
