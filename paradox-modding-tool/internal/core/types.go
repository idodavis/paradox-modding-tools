package core

import (
	parser "paradox-modding-tool/internal/interpreter"
)

// ############
// Merge options and result types
// ############

// MergeOptions configures how files are merged (additional entries, placement, key list, comment prefix).
type MergeOptions struct {
	// AddAdditionalEntries determines whether to add entries from the other file that don't exist in the base
	AddAdditionalEntries bool

	// EntryPlacement determines where to place added entries: "bottom" or "preserve_order"
	EntryPlacement string

	// KeyList is a list of keys to manually preserve from the other file (overrides conflict resolution)
	KeyList []string

	// CustomCommentPrefix is the prefix for comments to preserve from file B
	CustomCommentPrefix string
}

// FileSet represents a set of files to merge (root dir and relative paths).
type FileSet struct {
	RootDir      string   // Root directory for the file set
	Files        []string // List of relative file paths
	RelativePath string   // For single file operations
}

// MergeResult holds added/removed/changed/kept keys and any error for a merge.
type MergeResult struct {
	FilePath       string
	EntriesAdded   []string // Keys that were added from file B
	EntriesRemoved []string // Keys that were removed (not in file A)
	EntriesChanged []string // Keys that were replaced
	EntriesKept    []string // Keys that were kept from file A
	Error          error
}

// ModdedObject holds a parsed object expression and its leading comment.
type ModdedObject struct {
	ObjectExpr *parser.Expression
	Comment    string
}
