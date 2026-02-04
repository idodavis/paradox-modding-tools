package inventory

////////////////////////////////////////////////////////////
// Result types
////////////////////////////////////////////////////////////

// InventoryItem represents a single extracted game object with metadata.
type InventoryItem struct {
	// Key is the unique identifier for this object (e.g., character ID, event name)
	Key string `json:"key"`

	// Type is the object type (e.g., "characters", "events", "traits")
	Type string `json:"type"`

	// FilePath is the relative path to the file containing this object
	FilePath string `json:"filePath"`

	// LineStart is the line number where the object definition begins
	LineStart int `json:"lineStart"`

	// LineEnd is the line number where the object definition ends
	LineEnd int `json:"lineEnd"`

	// RawText contains the original text of the object definition
	RawText string `json:"rawText"`

	// PotentialRefs are identifiers found in this object's AST during extraction
	// These are resolved against known keys after extraction to populate References
	PotentialRefs []string `json:"-"` // Not serialized to JSON

	// References contains resolved references to other objects
	References []ObjectReference `json:"references,omitempty"`

	// Attributes lists attributes in the object body and whether they are present.
	Attributes map[string]bool `json:"attributes,omitempty"`
}

// ObjectReference represents a reference between two game objects
type ObjectReference struct {
	// TargetKey is the key of the referenced object
	TargetKey string `json:"targetKey"`

	// TargetType is the type of the referenced object
	TargetType string `json:"targetType"`

	// Context describes where the reference was found (e.g., property name like "holder", "father")
	Context string `json:"context"`

	// SourceFile is the file where the reference was found
	SourceFile string `json:"sourceFile"`

	// SourceLine is the line number where the reference was found
	SourceLine int `json:"sourceLine"`
}

// InventoryResult is used for export: one type's items (and optional metadata).
type InventoryResult struct {
	Type       string          `json:"type"`
	TotalCount int             `json:"totalCount"`
	Items      []InventoryItem `json:"items"`
	Errors     []string        `json:"errors,omitempty"`
}

// ExtractResult is returned by ExtractInventory: items keyed by type and any non-fatal parse errors.
type ExtractResult struct {
	Items  map[string][]InventoryItem `json:"items"`
	Errors []string                   `json:"errors,omitempty"`
}

// FilterState is the filter state for table filtering and export (same shape as frontend).
type FilterState struct {
	KeyText       string   `json:"keyText"`
	KeyMatchMode  string   `json:"keyMatchMode"`
	TypeNames     []string `json:"typeNames"`
	RefsValue     *int     `json:"refsValue,omitempty"`
	RefsMatchMode string   `json:"refsMatchMode"`
}

// FilteredSortedPage is the result of FilterAndSortPage: one page of items and total count.
type FilteredSortedPage struct {
	Items        []InventoryItem `json:"items"`
	TotalRecords int             `json:"totalRecords"`
}
