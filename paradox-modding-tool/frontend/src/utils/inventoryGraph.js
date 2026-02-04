/**
 * Build graph data for the reference graph (frontend-only).
 * itemsMap: { [type: string]: Array<{ key, type, references: Array<{ targetKey, targetType }> }> }
 * Returns { nodes, links, totalRefs, totalReferrers } with max REF_PAGE_SIZE (100) per slice.
 */
const REF_PAGE_SIZE = 100

function flattenItems(itemsMap) {
  const list = []
  if (!itemsMap) return list
  for (const [type, items] of Object.entries(itemsMap)) {
    for (const it of items || []) {
      list.push({ ...it, type })
    }
  }
  return list
}

function keyToId(type, key) {
  return `${type}:${key}`
}

export function buildGraphData(itemsMap, focusType, focusKey, refsOffset, refsLimit, referrersOffset, referrersLimit) {
  const limitRefs = refsLimit > 0 ? refsLimit : REF_PAGE_SIZE
  const limitReferrers = referrersLimit > 0 ? referrersLimit : REF_PAGE_SIZE

  const allItems = flattenItems(itemsMap)
  const byId = new Map()
  for (const it of allItems) {
    byId.set(keyToId(it.type, it.key), it)
  }

  const focusId = keyToId(focusType, focusKey)
  const focusItem = byId.get(focusId)
  if (!focusItem) {
    return { nodes: [], links: [], totalRefs: 0, totalReferrers: 0 }
  }

  // Referrers: who references the focus? Stored on focus: focusItem.references = list of { targetKey, targetType } where that is the SOURCE (who references me).
  const referrersList = focusItem.references || []
  const totalReferrers = referrersList.length
  const referrersSlice = referrersList.slice(referrersOffset, referrersOffset + limitReferrers)

  // Refs: who does the focus reference? We need reverse index: items that have focus in their references as the source.
  // Ref on target T has targetKey=source.key, targetType=source.type. So "focus references X" means X.references has entry with targetKey=focusKey, targetType=focusType.
  const refsList = []
  for (const it of allItems) {
    const refs = it.references || []
    for (const r of refs) {
      if (r.targetKey === focusKey && r.targetType === focusType) {
        refsList.push({ type: it.type, key: it.key })
        break
      }
    }
  }
  const totalRefs = refsList.length
  const refsSlice = refsList.slice(refsOffset, refsOffset + limitRefs)

  const categoryFocus = 0
  const categoryRefs = 1
  const categoryReferrers = 2
  const nodes = []
  const links = []
  const seenNodes = new Set()

  function addNode(item, category, value = 0) {
    const id = keyToId(item.type, item.key)
    if (seenNodes.has(id)) return id
    seenNodes.add(id)
    nodes.push({
      id,
      name: item.key,
      category,
      symbolSize: category === categoryFocus ? 40 : 24,
      value,
      itemData: item
    })
    return id
  }

  addNode(focusItem, categoryFocus, totalRefs + totalReferrers)

  for (const { type, key } of refsSlice) {
    const item = byId.get(keyToId(type, key))
    if (item) {
      const id = addNode(item, categoryRefs, 1)
      links.push({ source: focusId, target: id })
    }
  }
  for (const ref of referrersSlice) {
    const id = keyToId(ref.targetType, ref.targetKey)
    const item = byId.get(id)
    if (item) {
      addNode(item, categoryReferrers, 1)
      links.push({ source: id, target: focusId })
    }
  }

  return {
    nodes,
    links,
    totalRefs,
    totalReferrers
  }
}
