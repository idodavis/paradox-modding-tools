package inventory

// EnrichAllWithReferences resolves PotentialRefs and adds reference information to all inventory items
// References are stored on the TARGET item (the item being referenced), showing what references it
// This is called automatically at the end of ExtractInventory
func EnrichAllWithReferences(inventories map[string]*InventoryResult) {
	// Build index of all keys -> their inventory item pointer
	keyToItem := make(map[string]*InventoryItem)
	for _, inventory := range inventories {
		for i := range inventory.Items {
			keyToItem[inventory.Items[i].Key] = &inventory.Items[i]
		}
	}

	// For each item (source), check what it references
	// Then add a reference entry to the TARGET item
	for typeName, inventory := range inventories {
		for i := range inventory.Items {
			sourceItem := &inventory.Items[i]

			// Check each potential reference from this source
			for _, potentialRef := range sourceItem.PotentialRefs {
				// Skip self-references
				if potentialRef == sourceItem.Key {
					continue
				}

				// If this potential ref matches a known key, add a reference to the target
				if targetItem, exists := keyToItem[potentialRef]; exists {
					ref := ObjectReference{
						TargetKey:  sourceItem.Key,  // The source that references the target
						TargetType: typeName,        // Source's type
						Context:    "ast_reference",
						SourceFile: sourceItem.FilePath,
						SourceLine: sourceItem.LineStart,
					}
					targetItem.References = append(targetItem.References, ref)
				}
			}
		}
	}

	// Clear potential refs to save memory (they're no longer needed)
	for _, inventory := range inventories {
		for i := range inventory.Items {
			inventory.Items[i].PotentialRefs = nil
		}
	}
}
