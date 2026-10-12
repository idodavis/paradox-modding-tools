/**
 * The Markdown editor's toolbar, which the listing's description and a Markdown file's Rich view share.
 */
import type { EditorToolbarItem } from "@nuxt/ui";

/** Every mark and block the BBCode conversion carries, but links and images. */
export const MARKDOWN_TOOLBAR: EditorToolbarItem[][] = [
  [
    { kind: "heading", level: 1, icon: "i-lucide-heading-1", tooltip: { text: "Heading 1" } },
    { kind: "heading", level: 2, icon: "i-lucide-heading-2", tooltip: { text: "Heading 2" } },
    { kind: "heading", level: 3, icon: "i-lucide-heading-3", tooltip: { text: "Heading 3" } },
  ],
  [
    { kind: "mark", mark: "bold", icon: "i-lucide-bold", tooltip: { text: "Bold" } },
    { kind: "mark", mark: "italic", icon: "i-lucide-italic", tooltip: { text: "Italic" } },
    { kind: "mark", mark: "strike", icon: "i-lucide-strikethrough", tooltip: { text: "Strike" } },
    { kind: "mark", mark: "code", icon: "i-lucide-code", tooltip: { text: "Code" } },
  ],
  [
    { kind: "bulletList", icon: "i-lucide-list", tooltip: { text: "Bulleted list" } },
    { kind: "orderedList", icon: "i-lucide-list-ordered", tooltip: { text: "Numbered list" } },
    { kind: "blockquote", icon: "i-lucide-text-quote", tooltip: { text: "Quote" } },
    { kind: "codeBlock", icon: "i-lucide-square-code", tooltip: { text: "Code block" } },
    { kind: "horizontalRule", icon: "i-lucide-separator-horizontal", tooltip: { text: "Rule" } },
  ],
];
