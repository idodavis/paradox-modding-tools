/**
 * The system's folder and file pickers, for the forms' Browse buttons. A cancel gives "".
 */
import { Dialogs } from "@wailsio/runtime";

/** The images a thumbnail may be: Go's local.ThumbnailExts. */
export const IMAGE_FILTER = "*.png; *.gif; *.jpg; *.jpeg";

export function pickDirectory(title: string): Promise<string> {
  return picked(Dialogs.OpenFile({ Title: title, CanChooseDirectories: true, CanChooseFiles: false }));
}

/** A file; the picker opens in directory when given, else where any picker in the app was last used. */
export function pickFile(title: string, filter: string, directory?: string): Promise<string> {
  const filters = [{ DisplayName: filter, Pattern: filter }];
  return picked(Dialogs.OpenFile({ Title: title, CanChooseFiles: true, Filters: filters, Directory: directory }));
}

/** Where to save a file, named name to start with. */
export function pickSave(title: string, name: string, filter: string): Promise<string> {
  return picked(Dialogs.SaveFile({ Title: title, Filename: name, Filters: [{ DisplayName: filter, Pattern: filter }] }));
}

// Wails rejects a cancelled dialog ("cancelled by user"); any other failure still throws
function picked(choice: Promise<string>): Promise<string> {
  return choice.catch((error: unknown) => {
    if (String(error).includes("cancelled by user")) return "";
    throw error;
  });
}
