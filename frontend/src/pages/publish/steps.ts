/**
 * Publish's three steps and their titles.
 */
export type Step = "listing" | "release" | "review";
/** Each step's title, in the rail and as its page's heading. */
export const STEP_TITLES: Record<Step, string> = {
  listing: "Listing and description",
  release: "Version and change note",
  review: "Review and upload",
};
