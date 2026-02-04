package inventory

import (
	"sort"
	"strings"
)

// matchKeyFilter returns true if key passes the filter (keyText empty = pass all).
func matchKeyFilter(key, keyText, mode string) bool {
	if keyText == "" {
		return true
	}
	switch mode {
	case "contains":
		return strings.Contains(key, keyText)
	case "startsWith":
		return strings.HasPrefix(key, keyText)
	case "equals":
		return key == keyText
	default:
		return strings.Contains(key, keyText)
	}
}

// FilterItems returns items from result that match filterState, grouped by type.
// If filterState is nil, all items are included. Used by both export and table.
func FilterItems(result *ExtractResult, filterState *FilterState) map[string][]InventoryItem {
	if result == nil || result.Items == nil {
		return nil
	}
	out := make(map[string][]InventoryItem)
	typeSet := make(map[string]bool)
	if filterState != nil {
		for _, t := range filterState.TypeNames {
			typeSet[t] = true
		}
	}
	keyText := ""
	keyMatchMode := "contains"
	if filterState != nil {
		keyText = filterState.KeyText
		keyMatchMode = filterState.KeyMatchMode
	}
	for typeName, items := range result.Items {
		if filterState != nil && len(typeSet) > 0 && !typeSet[typeName] {
			continue
		}
		var filtered []InventoryItem
		for _, it := range items {
			if !matchKeyFilter(it.Key, keyText, keyMatchMode) {
				continue
			}
			if filterState != nil && filterState.RefsValue != nil {
				n := len(it.References)
				switch filterState.RefsMatchMode {
				case "gte":
					if n < *filterState.RefsValue {
						continue
					}
				case "lte":
					if n > *filterState.RefsValue {
						continue
					}
				case "eq":
					if n != *filterState.RefsValue {
						continue
					}
				}
			}
			filtered = append(filtered, it)
		}
		if len(filtered) > 0 {
			out[typeName] = filtered
		}
	}
	return out
}

// flattenItems turns a map type->items into a single slice (items already have Type set).
func flattenItems(byType map[string][]InventoryItem) []InventoryItem {
	var out []InventoryItem
	for _, items := range byType {
		out = append(out, items...)
	}
	return out
}

// sortItems sorts items in place by sortField; sortOrder 1 = ascending, -1 = descending.
func sortItems(items []InventoryItem, sortField string, sortOrder int) {
	if sortField == "" {
		sortField = "key"
	}
	asc := sortOrder >= 0
	sort.Slice(items, func(i, j int) bool {
		return lessByField(items[i], items[j], sortField, asc)
	})
}

func lessByField(a, b InventoryItem, sortField string, asc bool) bool {
	cmp := 0
	switch sortField {
	case "references":
		na, nb := len(a.References), len(b.References)
		if na < nb {
			cmp = -1
		} else if na > nb {
			cmp = 1
		} else {
			cmp = 0
		}
	case "type":
		if a.Type < b.Type {
			cmp = -1
		} else if a.Type > b.Type {
			cmp = 1
		} else {
			cmp = 0
		}
	case "filePath":
		if a.FilePath < b.FilePath {
			cmp = -1
		} else if a.FilePath > b.FilePath {
			cmp = 1
		}
	case "key":
		fallthrough
	default:
		if a.Key < b.Key {
			cmp = -1
		} else if a.Key > b.Key {
			cmp = 1
		}
	}
	if !asc {
		cmp = -cmp
	}
	return cmp < 0
}

// FilterAndSortPage filters and sorts the extract result, then returns the requested page.
// first is the 0-based index of the first item; rows is the page size.
func FilterAndSortPage(result *ExtractResult, filterState *FilterState, sortField string, sortOrder int, first, rows int) *FilteredSortedPage {
	filtered := FilterItems(result, filterState)
	flat := flattenItems(filtered)
	total := len(flat)
	sortItems(flat, sortField, sortOrder)
	if first < 0 {
		first = 0
	}
	if rows <= 0 {
		rows = 25
	}
	end := first + rows
	if end > total {
		end = total
	}
	var page []InventoryItem
	if first < total {
		page = flat[first:end]
	}
	return &FilteredSortedPage{Items: page, TotalRecords: total}
}

// FilterForExport filters ExtractResult by FilterState and returns a map suitable for export (e.g. JSON/CSV).
func FilterForExport(result *ExtractResult, filterState *FilterState) map[string]*InventoryResult {
	filtered := FilterItems(result, filterState)
	if filtered == nil {
		return nil
	}
	out := make(map[string]*InventoryResult)
	for typeName, items := range filtered {
		out[typeName] = &InventoryResult{Type: typeName, TotalCount: len(items), Items: items}
	}
	return out
}
