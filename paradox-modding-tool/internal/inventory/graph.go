package inventory

import "sort"

// BuildGraphData builds nodes and links for the reference graph from inventories.
// Links are "source references target": source = referencer, target = referenced.
// Called after EnrichAllWithReferences so item.References is populated.
func BuildGraphData(inventories map[string]*InventoryResult) *GraphData {
	types := sortedTypes(inventories)
	typeIdx := make(map[string]int)
	for i, t := range types {
		typeIdx[t] = i
	}

	nodes := []GraphNode{}
	links := []GraphLink{}
	linkSet := make(map[string]bool)

	for typeName, result := range inventories {
		categoryIdx := typeIdx[typeName]
		for _, item := range result.Items {
			nodeID := typeName + ":" + item.Key
			refCount := len(item.References)
			symbolSize := 10 + float64(refCount*2)
			if symbolSize > 40 {
				symbolSize = 40
			}
			nodes = append(nodes, GraphNode{
				ID:         nodeID,
				Name:       item.Key,
				Category:   categoryIdx,
				SymbolSize: symbolSize,
				Value:      refCount,
			})

			// item.References: who references this item (targetKey/targetType = referencer)
			// Link: source = referencer, target = this item
			for _, ref := range item.References {
				sourceID := ref.TargetType + ":" + ref.TargetKey
				targetID := typeName + ":" + item.Key
				linkKey := sourceID + "->" + targetID
				if !linkSet[linkKey] {
					linkSet[linkKey] = true
					links = append(links, GraphLink{Source: sourceID, Target: targetID})
				}
			}
		}
	}

	return &GraphData{Nodes: nodes, Links: links}
}

func sortedTypes(inventories map[string]*InventoryResult) []string {
	types := make([]string, 0, len(inventories))
	for t := range inventories {
		types = append(types, t)
	}
	sort.Strings(types)
	return types
}
