/**
 * Shared wiki article mount: Tailwind stamp, search highlight, in-pane links.
 */
import {
  nextTick,
  shallowRef,
  toValue,
  useTemplateRef,
  watch,
  type MaybeRefOrGetter,
} from "vue";
import {
  applyWikiTailwind,
  colorizePres,
  highlightWiki,
  resolveWikiLink,
  type WikiLinkHit,
} from "../wikiArticle";

/** The page an article belongs to, for resolving its in-pane wiki links. */
export type WikiArticlePage = { title: string; url: string };

/** Stamp, highlight, and click-navigate one sanitized wiki article. */
export function useWikiArticle(opts: {
  html: MaybeRefOrGetter<string | undefined>;
  pages: MaybeRefOrGetter<WikiArticlePage[]>;
  currentIndex: MaybeRefOrGetter<number>;
  query?: MaybeRefOrGetter<string>;
  enabled?: MaybeRefOrGetter<boolean>;
  onPage?: (hit: Extract<WikiLinkHit, { kind: "page" }>) => void;
}) {
  const articleEl = useTemplateRef<HTMLElement>("article");
  const tocActive = shallowRef("");
  const pendingAnchor = shallowRef("");

  watch(
    () => [
      toValue(opts.html),
      toValue(opts.query ?? ""),
      toValue(opts.enabled ?? true),
      articleEl.value,
    ],
    async () => {
      if (!toValue(opts.enabled ?? true)) return;
      await nextTick();
      const root = articleEl.value;
      if (!root) return;
      applyWikiTailwind(root);
      await colorizePres(root);
      highlightWiki(root, toValue(opts.query ?? ""));
      const pending = pendingAnchor.value;
      if (pending) {
        pendingAnchor.value = "";
        jumpTo(pending);
      }
    },
  );

  /** Scroll the article to a heading or element id. */
  function jumpTo(anchor: string): void {
    const root = articleEl.value;
    if (!root || !anchor) return;
    tocActive.value = anchor;
    root.querySelector(`#${CSS.escape(anchor)}`)?.scrollIntoView({
      block: "start",
    });
  }

  /** In-pane heading / tab jump; unknown URLs open in the browser. */
  function onWikiClick(ev: MouseEvent): void {
    const a = (ev.target as HTMLElement | null)?.closest("a");
    if (!a) return;
    const href = a.getAttribute("href") || a.href;
    if (!href) return;
    ev.preventDefault();
    const hit = resolveWikiLink(
      href,
      toValue(opts.pages),
      toValue(opts.currentIndex),
    );
    switch (hit.kind) {
      case "anchor":
        if (hit.anchor) jumpTo(hit.anchor);
        break;
      case "page":
        if (opts.onPage) {
          pendingAnchor.value = hit.anchor;
          opts.onPage(hit);
          break;
        }
        if (hit.anchor) jumpTo(hit.anchor);
        break;
      case "external":
        window.open(a.href, "_blank");
        break;
      default: {
        const _x: never = hit;
        return _x;
      }
    }
  }

  return { articleEl, tocActive, pendingAnchor, jumpTo, onWikiClick };
}
