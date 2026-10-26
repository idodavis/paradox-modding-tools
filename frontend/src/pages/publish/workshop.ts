/** This file holds Steam's Workshop addresses and saves a mod's upload as a zip. */
import { toValue, type MaybeRefOrGetter } from "vue";
import { useMutation } from "@pinia/colada";
import { Dialogs } from "@wailsio/runtime";

import * as app from "@services/app/service";
import * as steam from "@services/steam/service";

/** AGREEMENT_URL is the address of the Steam Workshop agreement. */
export const AGREEMENT_URL = "https://steamcommunity.com/sharedfiles/workshoplegalagreement";

/** ZIP is the file dialog's filter for zip files. */
export const ZIP = [{ DisplayName: "Zip", Pattern: "*.zip" }];

/** toPageURL returns the address of a Workshop item's Steam page. */
export function toPageURL(workshopId: string): string {
  return `https://steamcommunity.com/sharedfiles/filedetails/?id=${workshopId}`;
}

/** useZip returns a mutation that saves a mod's upload as a zip. A cancelled dialog saves nothing. */
export function useZip(modId: MaybeRefOrGetter<string>, saved?: (path: string) => void) {
  const toast = useToast();
  return useMutation({
    mutation: async (filename: string) => {
      const path = await Dialogs.SaveFile({
        Filename: `${filename || "mod"}.zip`,
        Filters: ZIP,
      });
      if (path) await steam.WriteZip(toValue(modId), path);
      return path;
    },
    onSuccess: (path) => {
      if (!path) return;
      saved?.(path);
      toast.add({
        title: "Zip saved",
        description: path,
        color: "success",
        actions: [{ label: "Show in the file manager", onClick: () => void app.ShowFolder(path) }],
      });
    },
  });
}
