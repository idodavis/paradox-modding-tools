/**
 * One sanitized wiki article in a pane: its code blocks drawn by Pierre, a find's hits marked and stepped
 * through one at a time, the heading in view marked as it scrolls, and its links followed in the pane. Its
 * look is `.art` (styles/article.css).
 */
import {
  nextTick,
  onScopeDispose,
  shallowRef,
  toValue,
  useTemplateRef,
  watch,
  type MaybeRefOrGetter,
} from "vue";
import { useEventListener } from "@vueuse/core";
import { Browser } from "@wailsio/runtime";
import type { File } from "@pierre/diffs";
import { codeView, drawCode } from "../lib/code";

/** The page an article belongs to, for resolving its in-pane wiki links. */
export type WikiArticlePage = { title: string; url: string };

/** A heading of the article, which a find jumps to before its first hit. */
type Heading = { line: string; anchor: string };

/** How a wiki <a> is followed in the pane. */
type WikiLinkHit =
  | { kind: "anchor"; anchor: string }
  | { kind: "page"; index: number; anchor: string }
  | { kind: "external" };

/** Draw, mark and click-navigate one sanitized wiki article. */
export function useWikiArticle(opts: {
  html: MaybeRefOrGetter<string | undefined>;
  pages: MaybeRefOrGetter<WikiArticlePage[]>;
  currentIndex: MaybeRefOrGetter<number>;
  sections?: MaybeRefOrGetter<Heading[] | null | undefined>;
  query?: MaybeRefOrGetter<string>;
  enabled?: MaybeRefOrGetter<boolean>;
  onPage?: (hit: Extract<WikiLinkHit, { kind: "page" }>) => void;
}) {
  const articleEl = useTemplateRef<HTMLElement>("article");
  const tocActive = shallowRef("");
  const pendingAnchor = shallowRef("");
  let views: { view: File; host: HTMLElement }[] = [];
  // the find's hits, how many, and which one is shown
  let marks: HTMLElement[] = [];
  const hits = shallowRef(0);
  const hit = shallowRef(0);

  // after each article or find: its code drawn, the hits marked, then a link's anchor, else the
  // first heading the find names, else its first hit shown; a heading's own hit is the one shown
  watch(
    () => [toValue(opts.html), toValue(opts.query ?? ""), toValue(opts.enabled ?? true), articleEl.value],
    async () => {
      if (!toValue(opts.enabled ?? true)) return;
      await nextTick();
      const root = articleEl.value;
      if (!root) return;
      drawCodeBlocks(root);
      const needle = toValue(opts.query ?? "").trim();
      highlightWiki(root, needle);
      marks = [...root.querySelectorAll<HTMLElement>("mark")];
      hits.value = marks.length;
      const anchor = pendingAnchor.value || firstSectionHit(toValue(opts.sections), needle);
      pendingAnchor.value = "";
      if (anchor) jumpTo(anchor);
      const named = anchor ? marks.findIndex((mark) => mark.closest(`#${CSS.escape(anchor)}`)) : -1;
      showHit(Math.max(named, 0), !anchor);
      spy();
    },
  );
  useEventListener(() => articleEl.value?.parentElement, "scroll", spy, { passive: true });

  /** The heading in view: the last one shown whose top has scrolled past the pane's. */
  function spy(): void {
    const root = articleEl.value;
    const top = root?.parentElement?.getBoundingClientRect().top;
    if (!root || top === undefined) return;
    let current = "";
    for (const heading of root.querySelectorAll<HTMLElement>("h1[id], h2[id], h3[id], h4[id]")) {
      if (!heading.offsetParent) continue;
      if (heading.getBoundingClientRect().top > top + 24) break;
      current = heading.id;
    }
    if (current) tocActive.value = current;
  }

  /** Show the find's hit i, counting round from either end, marked as the current one. */
  function showHit(i: number, scroll = true): void {
    if (!marks.length) return;
    marks[hit.value]?.classList.remove("current");
    hit.value = (i + marks.length) % marks.length;
    marks[hit.value].classList.add("current");
    if (scroll) marks[hit.value].scrollIntoView({ block: "center" });
  }
  onScopeDispose(() => views.forEach(({ view }) => view.cleanUp()));

  /** Each <pre> becomes read-only code; the views of an article no longer shown are let go. */
  function drawCodeBlocks(root: HTMLElement): void {
    for (const { view, host } of views) if (!host.isConnected) view.cleanUp();
    views = views.filter(({ host }) => host.isConnected);
    for (const pre of root.querySelectorAll("pre")) {
      const text = (pre.textContent ?? "").replace(/\n$/, "");
      if (!text.trim()) continue;
      const host = document.createElement("div");
      host.className = "code";
      pre.replaceWith(host);
      const view = codeView(false);
      drawCode(view, host, text, "snippet.txt");
      views.push({ view, host });
    }
  }

  /** Scroll the article to a heading or element id. */
  function jumpTo(anchor: string): void {
    const root = articleEl.value;
    if (!root || !anchor) return;
    tocActive.value = anchor;
    root.querySelector(`#${CSS.escape(anchor)}`)?.scrollIntoView({ block: "start" });
  }

  /** In-pane heading / tab jump; unknown URLs open in the browser. */
  function onWikiClick(ev: MouseEvent): void {
    const a = (ev.target as HTMLElement | null)?.closest("a");
    if (!a) return;
    const href = a.getAttribute("href") || a.href;
    if (!href) return;
    ev.preventDefault();
    const hit = resolveWikiLink(href, toValue(opts.pages), toValue(opts.currentIndex));
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
        openUrl(a.href);
        break;
      default: {
        const _x: never = hit;
        return _x;
      }
    }
  }

  return { articleEl, tocActive, hits, hit, showHit, jumpTo, onWikiClick, openUrl };
}

/** Open a page in the system's browser, not a window of the app. */
function openUrl(url: string): void {
  void Browser.OpenURL(url);
}

/** First heading anchor whose text holds the needle, or empty. */
function firstSectionHit(sections: Heading[] | null | undefined, needle: string): string {
  const q = needle.toLowerCase();
  if (!q) return "";
  return sections?.find((s) => s.line.toLowerCase().includes(q))?.anchor ?? "";
}

/** Mark case-insensitive hits; code is left alone. */
function highlightWiki(root: HTMLElement, needle: string): void {
  clearWikiMarks(root);
  if (!needle) return;
  const lower = needle.toLowerCase();
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT, {
    acceptNode(node) {
      const p = (node as Text).parentElement;
      if (!p || p.closest("pre, code")) return NodeFilter.FILTER_REJECT;
      return (node.textContent ?? "").toLowerCase().includes(lower)
        ? NodeFilter.FILTER_ACCEPT
        : NodeFilter.FILTER_SKIP;
    },
  });
  const hits: Text[] = [];
  while (walker.nextNode()) hits.push(walker.currentNode as Text);
  for (const text of hits) wrapHits(text, needle);
}

/** Unwrap previous search marks. */
function clearWikiMarks(root: HTMLElement): void {
  root.querySelectorAll("mark").forEach((mark) => {
    const parent = mark.parentNode;
    if (!parent) return;
    while (mark.firstChild) parent.insertBefore(mark.firstChild, mark);
    parent.removeChild(mark);
    parent.normalize();
  });
}

function wrapHits(node: Text, needle: string): void {
  const src = node.textContent ?? "";
  const lower = src.toLowerCase();
  const q = needle.toLowerCase();
  const frag = document.createDocumentFragment();
  let i = 0;
  while (i < src.length) {
    const at = lower.indexOf(q, i);
    if (at < 0) {
      frag.appendChild(document.createTextNode(src.slice(i)));
      break;
    }
    if (at > i) frag.appendChild(document.createTextNode(src.slice(i, at)));
    const mark = document.createElement("mark");
    mark.textContent = src.slice(at, at + needle.length);
    frag.appendChild(mark);
    i = at + needle.length;
  }
  node.parentNode?.replaceChild(frag, node);
}

/** Resolve a wiki href against the pages currently loaded in the pane. */
function resolveWikiLink(href: string, pages: WikiArticlePage[], currentIndex: number): WikiLinkHit {
  const raw = href.trim();
  if (!raw) return { kind: "external" };
  const base = pages[currentIndex]?.url || "https://local.invalid/";
  let url: URL;
  try {
    url = new URL(raw, base);
  } catch {
    return { kind: "external" };
  }
  const anchor = decodeURIComponent(url.hash.replace(/^#/, ""));
  const pathTitle = wikiPathTitle(url);
  const idx = pages.findIndex((p) => wikiSamePage(p, url, pathTitle));
  if (idx >= 0) {
    if (idx === currentIndex) return { kind: "anchor", anchor };
    return { kind: "page", index: idx, anchor };
  }
  if (!pathTitle && anchor) return { kind: "anchor", anchor };
  return { kind: "external" };
}

function wikiPathTitle(url: URL): string {
  const parts = url.pathname.split("/").filter(Boolean);
  const last = parts.at(-1) ?? "";
  if (!last || last === "wiki" || last === "index.php") return "";
  try {
    return decodeURIComponent(last).replace(/_/g, " ");
  } catch {
    return last.replace(/_/g, " ");
  }
}

function wikiSamePage(p: WikiArticlePage, href: URL, pathTitle: string): boolean {
  const want = normWiki(pathTitle);
  if (want && normWiki(p.title) === want) return true;
  try {
    return normWikiPath(new URL(p.url)) === normWikiPath(href);
  } catch {
    return false;
  }
}

function normWiki(s: string): string {
  return s.trim().toLowerCase().replace(/_/g, " ");
}

function normWikiPath(u: URL): string {
  return u.pathname.replace(/\/+$/, "").toLowerCase();
}
