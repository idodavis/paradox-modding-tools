/**
 * The system's folder and file pickers, for the forms' Browse buttons. A cancel gives "".
 */
import { Dialogs } from "@wailsio/runtime";

/** The images a thumbnail may be. */
export const IMAGE_FILTER = "*.png; *.jpg; *.jpeg; *.svg";

export function pickDirectory(title: string): Promise<string> {
  return Dialogs.OpenFile({ Title: title, CanChooseDirectories: true, CanChooseFiles: false });
}

export function pickFile(title: string, filter: string): Promise<string> {
  return Dialogs.OpenFile({ Title: title, CanChooseFiles: true, Filters: [{ DisplayName: filter, Pattern: filter }] });
}
