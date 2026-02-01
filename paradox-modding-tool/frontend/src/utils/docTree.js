/**
 * Builds a PrimeVue TreeTable tree from flat doc file paths.
 * Hierarchy follows the path but only 2 folder levels deep; deeper paths are
 * flattened so the leaf label is the remainder (e.g. "sub/bar.info").
 * @param {Array<{ relativePath: string, fullPath?: string }>} entries
 * @returns {Array<{ key: string, data: { name: string, relativePath?: string }, children?: Array }>}
 */
export function buildDocTree(entries) {
  if (!entries?.length) return []

  const root = new Map() // segment -> node

  for (const entry of entries) {
    const path = entry.relativePath?.trim() || ''
    if (!path) continue

    const parts = path.split('/').filter(Boolean)
    if (parts.length === 0) continue

    if (parts.length <= 2) {
      // 0: file only, 1: folder/file, 2: folder/folder/file
      const leafLabel = parts[parts.length - 1]
      const leafKey = 'leaf:' + path
      if (parts.length === 1) {
        const key = 'root:' + path
        if (!root.has(key)) {
          root.set(key, {
            key,
            data: { name: leafLabel, relativePath: path },
            children: undefined
          })
        }
        continue
      }
      const seg0 = parts[0]
      let node0 = root.get(seg0)
      if (!node0) {
        node0 = { key: seg0, data: { name: seg0 }, children: [] }
        root.set(seg0, node0)
      }
      node0.children.push({
        key: leafKey,
        data: { name: leafLabel, relativePath: path },
        children: undefined
      })
      continue
    }

    // 3+ parts: first two are folder levels, rest flattened into leaf label
    const seg0 = parts[0]
    const seg1 = parts[1]
    const leafLabel = parts.slice(2).join('/')
    const leafKey = 'leaf:' + path

    let node0 = root.get(seg0)
    if (!node0) {
      node0 = { key: seg0, data: { name: seg0 }, children: [] }
      root.set(seg0, node0)
    }

    let node1 = node0.children.find((c) => c.data?.name === seg1 && c.children)
    if (!node1) {
      node1 = { key: seg0 + '/' + seg1, data: { name: seg1 }, children: [] }
      node0.children.push(node1)
    }

    node1.children.push({
      key: leafKey,
      data: { name: leafLabel, relativePath: path },
      children: undefined
    })
  }

  // Sort and dedupe leaves under same parent (by key)
  const result = []
  for (const node of root.values()) {
    if (node.children?.length) {
      const seen = new Set()
      node.children = node.children.filter((c) => {
        const k = c.key
        if (seen.has(k)) return false
        seen.add(k)
        return true
      })
      node.children.sort((a, b) => (a.data?.name || '').localeCompare(b.data?.name || ''))
    }
    result.push(node)
  }
  result.sort((a, b) => (a.data?.name || '').localeCompare(b.data?.name || ''))
  return result
}
