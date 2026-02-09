import { createHighlighter, type Highlighter } from "shiki";

let highlighterPromise: Promise<Highlighter> | null = null;

export async function getHighlighter(): Promise<Highlighter> {
  if (!highlighterPromise) {
    highlighterPromise = createHighlighter({
      themes: ["one-dark-pro"],
      langs: ["hcl", "json", "yaml", "typescript", "html", "css", "markdown", "xml", "bash", "python", "c++", "go", "rust", "java"],
    });
  }
  return highlighterPromise;
}