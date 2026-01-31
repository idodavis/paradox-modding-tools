/**
 * Single filter pass: by type and search text.
 * Returns both map (for export) and flat list with uniqueId (for table).
 */
function filterCore(inventory, { filterText = '', filterTypes = [] }) {
  const map = {}
  const flat = []
  const search = filterText ? filterText.toLowerCase() : ''
  let id = 0

  for (const [type, inv] of Object.entries(inventory || {})) {
    if (filterTypes.length > 0 && !filterTypes.includes(type)) continue
    let items = inv.items || []
    if (search) {
      items = items.filter(
        (item) =>
          item.key?.toLowerCase().includes(search) ||
          item.filePath?.toLowerCase().includes(search)
      )
    }
    if (items.length > 0) {
      map[type] = { ...inv, items, totalCount: items.length }
      for (const item of items) {
        flat.push({ ...item, uniqueId: `${type}-${item.key}-${id++}` })
      }
    }
  }
  return { map, flat }
}

/** Flat list of items (for ResultsTable). */
export function filterInventoryItems(inventory, opts) {
  return filterCore(inventory, opts).flat
}

/** Map of type -> { items, totalCount } (for ExportImportDialog). */
export function filterInventory(inventory, opts) {
  return filterCore(inventory, opts).map
}

export function countInventoryItems(inventory) {
  return Object.values(inventory || {}).reduce((sum, r) => sum + (r.items?.length || 0), 0)
}

export function countReferences(inventory) {
  return Object.values(inventory || {}).reduce(
    (sum, r) => sum + (r.items?.reduce((s, i) => s + (i.references?.length || 0), 0) || 0),
    0
  )
}
