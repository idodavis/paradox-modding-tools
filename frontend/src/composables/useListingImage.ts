/**
 * A mod's listing image replaced by a picked file: copied into the mod, named in its descriptor and made
 * the workspace's picture of the mod, in one call. Settings and Publish change it.
 */
import { toValue, type MaybeRefOrGetter } from "vue";
import { useMutation, useQueryCache } from "@pinia/colada";
import { SetModListingImage } from "@services/workspaceservice";
import { IMAGE_FILTER, pickFile } from "../lib/nativeDialog";
import { fileKeys } from "../queries";

export function useListingImage(workspaceId: MaybeRefOrGetter<string>) {
  const queryCache = useQueryCache();
  const { mutate, isLoading } = useMutation({
    mutation: ({ modId, src }: { modId: string; src: string }) => SetModListingImage(toValue(workspaceId), modId, src),
    // the new image is often at the old one's path
    onSettled: (image) => image && queryCache.invalidateQueries({ key: fileKeys.image(image) }),
  });

  /** Pick an image and make it the mod's listing image. */
  async function change(modId: string): Promise<void> {
    const src = await pickFile("The listing image", IMAGE_FILTER);
    if (src) mutate({ modId, src });
  }
  return { change, changing: isLoading };
}
