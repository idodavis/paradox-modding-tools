package inventory

import (
	"sort"
	"strings"
)

// matchKeyFilter returns true if key passes the filter (keyText empty = pass all).
func keyFilterMatchMode(key, keyText, mode string) bool {
	if keyText == "" {
		return true
	}
	switch mode {
	case "CONTAINS":
		return strings.Contains(key, keyText)
	case "EQUALS":
		return key == keyText
	case "NOT_CONTAINS":
		return !strings.Contains(key, keyText)
	case "NOT_EQUALS":
		return key != keyText
	default:
		return strings.Contains(key, keyText)
	}
}

// FilterItems filters the stored extract result based on the filterState.
func FilterItems(filterState *FilterState) (map[string][]InventoryItem, error) {
	// If filterState is nil, nothing is done.
	if filterState == nil {
		return nil, nil
	}
	inventory := GetStored()

	out := make(map[string][]InventoryItem)
	typeSet := make(map[string]bool)
	for _, t := range filterState.TypeNames {
		typeSet[t] = true
	}

	keyText := filterState.KeyText
	keyMatchMode := filterState.KeyMatchMode

	for typeName, items := range inventory.Items {
		if len(typeSet) > 0 && !typeSet[typeName] {
			continue
		}
		var filtered []InventoryItem
		for _, it := range items {
			if !keyFilterMatchMode(it.Key, keyText, keyMatchMode) {
				continue
			}
			if filterState.RefsValue != nil {
				n := len(it.References)
				switch filterState.RefsMatchMode {
				case "GREATER_THAN_OR_EQUAL_TO":
					if n < *filterState.RefsValue {
						continue
					}
				case "EQUALS":
					if n != *filterState.RefsValue {
						continue
					}
				case "LESS_THAN_OR_EQUAL_TO":
					if n > *filterState.RefsValue {
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
	return out, nil
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

// TODO: Remove all filtering and sorting as AG-Grid now handles this in the front-end....beutifully.
// FilterAndSortPage filters and sorts the extract result, then returns the requested page.
// first is the 0-based index of the first item; rows is the page size.
func FilterAndSortPage(filterState *FilterState, sortField string, sortOrder int, first, rows int) *FilteredSortedPage {
	filtered, err := FilterItems(filterState)
	if err != nil {
		return nil
	}

	flat := flattenItems(filtered)
	total := len(flat)
	sortItems(flat, sortField, sortOrder)
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

// FilterForExport filters the stored ExtractResult by FilterState and returns a map suitable for export (e.g. JSON/CSV).
func FilterForExport(filterState *FilterState) map[string]*FilterSortResult {
	filtered, err := FilterItems(filterState)
	if err != nil {
		return nil
	}
	out := make(map[string]*FilterSortResult)
	for typeName, items := range filtered {
		out[typeName] = &FilterSortResult{Type: typeName, TotalCount: len(items), Items: items}
	}
	return out
}
