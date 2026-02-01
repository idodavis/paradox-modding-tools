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

export async function copyToClipboard(content) {
  try {
    await navigator.clipboard.writeText(content)
  } catch (err) {
    console.error('Failed to copy:', err)
  }
}

export function shortenPath(path) {
  if (!path) return ''
  const parts = path.split(/[/\\]/)
  if (parts.length > 3) {
    return '.../' + parts.slice(-3).join('/')
  }
  return path
}

export function fileNameFromPath(path) {
  if (!path) return ''
  return path.split(/[/\\]/).pop()
}