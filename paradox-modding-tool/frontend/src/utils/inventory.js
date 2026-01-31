/**
 * Filter state for table/export. Same shape used by ResultsTable and for export.
 * Match mode values match PrimeVue FilterMatchMode (e.g. 'contains', 'startsWith', 'gte', 'gt').
 * @typedef {{
 *   keyText: string,
 *   keyMatchMode: string,
 *   typeNames: string[],
 *   refsValue: number | null,
 *   refsMatchMode: string
 * }} InventoryFilterState
 */

/** Text match helpers (same semantics as PrimeVue FilterService). Value and filter are compared lowercased. */
const textMatch = {
  startsWith(value, filter) {
    return (value || '').toLowerCase().startsWith((filter || '').toLowerCase())
  },
  contains(value, filter) {
    return (value || '').toLowerCase().includes((filter || '').toLowerCase())
  },
  notContains(value, filter) {
    return !textMatch.contains(value, filter)
  },
  endsWith(value, filter) {
    return (value || '').toLowerCase().endsWith((filter || '').toLowerCase())
  },
  equals(value, filter) {
    return (value || '').toLowerCase() === (filter || '').toLowerCase()
  },
  notEquals(value, filter) {
    return !textMatch.equals(value, filter)
  }
}

/** Numeric match helpers (same semantics as PrimeVue). */
const numericMatch = {
  lt(value, filter) {
    const n = Number(value)
    const f = Number(filter)
    return !Number.isNaN(n) && !Number.isNaN(f) && n < f
  },
  lte(value, filter) {
    const n = Number(value)
    const f = Number(filter)
    return !Number.isNaN(n) && !Number.isNaN(f) && n <= f
  },
  gt(value, filter) {
    const n = Number(value)
    const f = Number(filter)
    return !Number.isNaN(n) && !Number.isNaN(f) && n > f
  },
  gte(value, filter) {
    const n = Number(value)
    const f = Number(filter)
    return !Number.isNaN(n) && !Number.isNaN(f) && n >= f
  },
  equals(value, filter) {
    const n = Number(value)
    const f = Number(filter)
    return !Number.isNaN(n) && !Number.isNaN(f) && n === f
  },
  notEquals(value, filter) {
    return !numericMatch.equals(value, filter)
  }
}

/**
 * Single filter: flatten inventory to rows, apply filters (with match modes), return flat list (for table) and map (for export).
 * @param {Record<string, { items: Array, type?: string }>} inventory - Grouped inventory from API
 * @param {InventoryFilterState} filterState - { keyText, keyMatchMode, typeNames, refsValue, refsMatchMode }
 * @returns {{ flat: Array<{ uniqueId: string, ... }>, map: Record<string, { type: string, totalCount: number, items: Array }> }}
 */
export function applyInventoryFilter(inventory, filterState = {}) {
  const keyFilter = (filterState.keyText || '').trim()
  const keyMode = filterState.keyMatchMode || 'contains'
  const typeNames = Array.isArray(filterState.typeNames) ? filterState.typeNames : []
  const refsFilter = filterState.refsValue != null && filterState.refsValue !== '' ? Number(filterState.refsValue) : null
  const refsMode = filterState.refsMatchMode || 'gte'

  const flat = []
  const map = {}
  let id = 0

  const textFn = textMatch[keyMode] || textMatch.contains
  const numericFn = numericMatch[refsMode] || numericMatch.gte

  for (const [typeName, typeInv] of Object.entries(inventory || {})) {
    const items = typeInv?.items || []
    for (const item of items) {
      const row = {
        ...item,
        uniqueId: `${typeName}-${item.key}-${id++}`,
        references: item.references || []
      }
      // Key filter (text match mode)
      if (keyFilter && !textFn(row.key, keyFilter)) continue
      // Type filter (IN: row.type must be in typeNames)
      if (typeNames.length > 0 && !typeNames.includes(row.type)) continue
      // Refs filter (numeric match mode)
      if (refsFilter != null) {
        const count = (row.references && row.references.length) || 0
        if (!numericFn(count, refsFilter)) continue
      }
      flat.push(row)
      if (!map[typeName]) {
        map[typeName] = { type: typeName, totalCount: 0, items: [] }
      }
      map[typeName].items.push(row)
      map[typeName].totalCount = map[typeName].items.length
    }
  }

  return { flat, map }
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
