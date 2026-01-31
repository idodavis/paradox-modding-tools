import { BrowserService } from '../../bindings/paradox-modding-tool/index.js'

/**
 * Parse a newline-separated list of paths into an array of trimmed non-empty strings.
 * Used for file/folder path inputs across FileSelector, MergeTool, ComparisonTool.
 */
export function parsePathList(text) {
  if (text == null || typeof text !== 'string') return []
  return text.split(/\r?\n/).map((p) => p.trim()).filter(Boolean)
}

// Open a URL in the browser.
export function openURLInBrowser(url) {
  BrowserService.OpenURL(url).catch(() => {})
}