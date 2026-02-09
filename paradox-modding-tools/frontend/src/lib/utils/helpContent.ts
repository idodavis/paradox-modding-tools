export type HelpPageId =
  | 'hub'
  | 'comparison'
  | 'merge'
  | 'inventory'
  | 'modding-docs'
  | 'settings'

export interface HelpContent {
  title: string
  body: string
}

const content: Record<HelpPageId, HelpContent> = {
  hub: {
    title: 'Hub',
    body: 'News shows latest patch notes. Choose a tool: Modding Docs, File Compare, Script Merger, or Object Inventory.'
  },
  comparison: {
    title: 'Comparison Tool',
    body: 'Compare two file sets or directories by relative path, or compare any two files. Use the tabs to switch between Vanilla vs mod, Two sets/directories, or Any two files.'
  },
  merge: {
    title: 'Merge Tool',
    body: 'Merge Paradox script files (base + mod). Use the tabs for Vanilla vs mod, Two sets/directories, or Any two files. For "Any two files", the output is chosen via Save dialog.'
  },
  inventory: {
    title: 'Object Inventory',
    body: 'Extract and explore game objects from script files. Filter by type, view references and dependencies. Export or import inventory data.'
  },
  'modding-docs': {
    title: 'Modding Docs',
    body: 'Browse script docs (.info for CK3, readme.txt for EU5) from your game install. Use Rescan after a game update. Embedded modding wiki opens in browser if framing is blocked.'
  },
  settings: {
    title: 'Settings',
    body: 'Set game install directories for CK3 and EU5. These paths are used by Modding Docs, Compare (vanilla vs mod), and Merge (vanilla vs mod). The current game is selected in the header.'
  }
}

export function getHelpForPage(page: string): HelpContent {
  return content[page as HelpPageId] ?? { title: 'Help', body: 'No help for this page.' }
}
