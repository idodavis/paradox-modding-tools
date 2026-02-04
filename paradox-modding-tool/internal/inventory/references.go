package inventory

////////////////////////////////////////////////////////////
// Reference enrichment
////////////////////////////////////////////////////////////

// EnrichAllWithReferences resolves PotentialRefs and adds reference information to all inventory items.
// References are stored on the TARGET item (the item being referenced), showing what references it.
// items is keyed by object type; each value is a slice of items for that type (may be modified in place).
// Multiple items can share the same key (e.g. different types); all such targets receive the ref.
func EnrichAllWithReferences(items map[string][]InventoryItem) {
	// Index key -> all items with that key (same key can exist in different types)
	keyToItems := make(map[string][]*InventoryItem)
	for _, list := range items {
		for i := range list {
			k := list[i].Key
			keyToItems[k] = append(keyToItems[k], &list[i])
		}
	}
	for typeName, list := range items {
		for i := range list {
			sourceItem := &list[i]

			seen := make(map[string]bool)
			for _, potentialRef := range sourceItem.PotentialRefs {
				if potentialRef == sourceItem.Key {
					continue
				}
				if seen[potentialRef] {
					continue
				}
				seen[potentialRef] = true
				targets := keyToItems[potentialRef]
				ref := ObjectReference{
					TargetKey:  sourceItem.Key,
					TargetType: typeName,
					Context:    "ast_reference",
					SourceFile: sourceItem.FilePath,
					SourceLine: sourceItem.LineStart,
				}
				for _, targetItem := range targets {
					targetItem.References = append(targetItem.References, ref)
				}
			}
			sourceItem.PotentialRefs = nil
		}
	}
}
