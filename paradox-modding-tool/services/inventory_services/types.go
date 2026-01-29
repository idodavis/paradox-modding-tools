package inventory_services

// ObjectTypeConfig defines how to identify and extract a specific type of game object
type ObjectTypeConfig struct {
	// Name is the display name for this object type
	Name string `json:"name"`

	// Paths are the relative directory paths to scan for files of this type
	Paths []string `json:"paths"`

	// KeyPattern defines how to identify object keys:
	// - "numeric": keys must be numeric (e.g., character IDs like 1001)
	// - "prefixed": keys must start with specific prefixes (e.g., e_, k_, d_ for titles)
	// - "namespaced": keys follow namespace.id pattern (e.g., events)
	// - "keyword_prefixed": keys start with a keyword (e.g., "scripted_trigger name")
	// - "any": accept any key
	KeyPattern string `json:"keyPattern"`

	// KeyPrefixes are the valid prefixes when KeyPattern is "prefixed"
	KeyPrefixes []string `json:"keyPrefixes,omitempty"`

	// KeyKeywords are the keywords when KeyPattern is "keyword_prefixed"
	// The actual object name follows the keyword (e.g., "scripted_trigger my_trigger")
	KeyKeywords []string `json:"keyKeywords,omitempty"`

	// InlinePaths are additional paths to scan for inline definitions
	// (e.g., scripted_triggers defined inline in event files)
	InlinePaths []string `json:"inlinePaths,omitempty"`

	// InlineKeyPattern is the pattern used for inline definitions (if different from KeyPattern)
	// e.g., "keyword_prefixed" for scripted_trigger definitions in event files
	InlineKeyPattern string `json:"inlineKeyPattern,omitempty"`

	// InlineKeyKeywords are the keywords for inline keyword_prefixed patterns
	InlineKeyKeywords []string `json:"inlineKeyKeywords,omitempty"`

	// RelatedTypes lists other object types that may reference this type
	// Used for deep inventory link tracking
	RelatedTypes []string `json:"relatedTypes,omitempty"`
}

// InventoryItem represents a single extracted game object with metadata
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

	// Links contains references to/from other objects (populated in deep mode only)
	Links []ObjectLink `json:"links,omitempty"`
}

// ObjectLink represents a reference between two game objects
type ObjectLink struct {
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

// InventoryResult contains the results of an inventory operation
type InventoryResult struct {
	// Type is the object type that was inventoried
	Type string `json:"type"`

	// TotalCount is the total number of objects found
	TotalCount int `json:"totalCount"`

	// Items contains all extracted objects
	Items []InventoryItem `json:"items"`

	// Errors contains any non-fatal errors encountered during extraction
	Errors []string `json:"errors,omitempty"`
}

// InventoryConfig holds the complete configuration for all object types
type InventoryConfig struct {
	// ObjectTypes maps type identifiers to their configurations
	ObjectTypes map[string]ObjectTypeConfig `json:"objectTypes"`
}
