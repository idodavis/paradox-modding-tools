package services

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	parser "paradox-modding-tools/services/internal/interpreter"
)

// ############
// MergeService
// ############

// MergeService merges matching script files from two path sets into an output directory using core merge logic.
type MergeService struct {
	FileService *FileService
}

// MergerOptions configures how files are merged (JSON-safe for bindings)
type MergerOptions struct {
	AddAdditionalEntries     bool     `json:"addAdditionalEntries"`
	ManualConflictResolution bool     `json:"manualConflictResolution"`
	EntryPlacement           string   `json:"entryPlacement"`
	KeyList                  []string `json:"keyList"`
	MatchByFilenameOnly      bool     `json:"matchByFilenameOnly"`
	IncludePathPattern       string   `json:"includePathPattern"`
	ExcludePathPattern       string   `json:"excludePathPattern"`
	OutputFilename           string   `json:"outputFilename"`
	OutputFileSuffix         string   `json:"outputFileSuffix"` // e.g. "_merged" meaning: events/foo.txt -> events/foo_merged.txt
}

// PreviewItem is a single file match for the merge preview (JSON-safe for bindings)
type PreviewItem struct {
	RelPath        string `json:"relPath"`
	PathA          string `json:"pathA"`
	PathB          string `json:"pathB"`
	OutputPath     string `json:"outputPath"`
	WouldOverwrite bool   `json:"wouldOverwrite"`
}

// FileMergeResult is the result of merging one file (JSON-safe for bindings)
type FileMergeResult struct {
	FilePath          string             `json:"filePath"`
	FileAPath         string             `json:"fileAPath"`
	FileBPath         string             `json:"fileBPath"`
	OutputPath        string             `json:"outputPath"`
	Changed           int                `json:"changed"`
	Added             int                `json:"added"`
	Removed           int                `json:"removed"`
	EntriesChanged    []string           `json:"entriesChanged,omitempty"`
	EntriesAdded      []string           `json:"entriesAdded,omitempty"`
	ResolvedConflicts []ResolvedConflict `json:"resolvedConflicts,omitempty"`
	Error             string             `json:"error,omitempty"`
}

// ResolvedConflict records a conflict that was auto-resolved (for report/audit).
type ResolvedConflict struct {
	Key      string `json:"key"`
	UsedSide string `json:"usedSide"` // "A" or "B"
	Reason   string `json:"reason"`   // "directive", "keyList", "default"
}

// MergeConflictChunk is a unit of content for the assisted merge editor (JSON-safe for bindings).
type MergeConflictChunk struct {
	Type  string `json:"type"`  // "unchanged" or "conflict"
	Key   string `json:"key"`   // object key when type is "conflict"
	Text  string `json:"text"`  // for unchanged
	TextA string `json:"textA"` // for conflict
	TextB string `json:"textB"` // for conflict
}

// scriptObject represents a parsed top-level entry (assignment or object) with its comments.
type scriptObject struct {
	Key        string
	RawText    string   // Full text including comments
	ValueText  string   // Just the value part (for normalization)
	Comments   []string // Preceding comments
	PreferSide string   // "A" or "B" parsed from directives
}

func buildCollectFilter(options MergerOptions) FileCollectorFilter {
	f := FileCollectorFilter{Extensions: []string{".txt"}}
	if options.IncludePathPattern != "" {
		f.IncludePath = options.IncludePathPattern
	}
	if options.ExcludePathPattern != "" {
		f.ExcludePath = options.ExcludePathPattern
	}
	return f
}

func outputPathWithSuffix(outputDir, relPath, suffix string) string {
	if suffix == "" {
		return filepath.Join(outputDir, relPath)
	}
	dir := filepath.Dir(relPath)
	base := filepath.Base(relPath)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	newBase := name + suffix + ext
	if filepath.Ext(newBase) != ".txt" {
		newBase = newBase + ".txt"
	}
	return filepath.Join(outputDir, dir, newBase)
}

var (
	precedenceRe      = regexp.MustCompile(`(?i)(?:PREFER|USE|MERGE)\s*:\s*([AB])|(?i)(?:PROTECT|KEEP)`)
	commentStripRe    = regexp.MustCompile(`(?m)#.*$`)
	whitespaceStripRe = regexp.MustCompile(`\s+`)
	utf8BOM           = "\uFEFF"
)

func parsePrecedenceFromComment(comment string) string {
	comment = strings.TrimSpace(strings.TrimPrefix(comment, "#"))
	if m := precedenceRe.FindStringSubmatch(comment); len(m) > 0 {
		if len(m) > 1 && m[1] != "" {
			return strings.ToUpper(m[1])
		}
		return "A" // PROTECT/KEEP -> keep from A
	}
	return ""
}

// normalize removes comments and collapses whitespace to prevent false positives.
func normalize(text string) string {
	noComments := commentStripRe.ReplaceAllString(text, "")
	return whitespaceStripRe.ReplaceAllString(noComments, "")
}

// areSemanticallyEqual checks if two entities are effectively the same logic.
func areSemanticallyEqual(textA, textB string) bool {
	if textA == textB {
		return true
	}
	return normalize(textA) == normalize(textB)
}

func parseFileObjects(path string) ([]scriptObject, string, error) {
	f, err := parser.ParseFile(path)
	if err != nil {
		return nil, "", err
	}

	var objects []scriptObject
	var pendingPreamble strings.Builder
	var pendingComments []string

	lineEnding := "\n"

	for _, entry := range f.Entries {
		// Accumulate raw text for everything (comments, whitespace, expressions)
		rawText := entry.GetRawText()
		if lineEnding == "\n" && strings.Contains(rawText, "\r\n") {
			lineEnding = "\r\n"
		}
		pendingPreamble.WriteString(rawText)

		if c := entry.Comment; c != "" {
			pendingComments = append(pendingComments, c)
		}

		if expr := entry.Expression; expr != nil && expr.Key != "" {
			key := expr.Key
			rawExpr := expr.GetRawText()

			// The preamble includes the expression's raw text because we appended entry.GetRawText() above.
			// So fullText is everything since the last object (comments + whitespace + this object).
			fullText := pendingPreamble.String()

			prefer := ""
			for _, pc := range pendingComments {
				if p := parsePrecedenceFromComment(pc); p != "" {
					prefer = p
					break
				}
			}

			objects = append(objects, scriptObject{
				Key:        key,
				RawText:    fullText,
				ValueText:  rawExpr,
				Comments:   pendingComments,
				PreferSide: prefer,
			})
			pendingPreamble.Reset()
			pendingComments = nil
		}
	}

	// Handle trailing content (comments/whitespace at EOF)
	if pendingPreamble.Len() > 0 {
		objects = append(objects, scriptObject{
			RawText: pendingPreamble.String(),
		})
	}

	bom := utf8BOM
	if f.BOM != "" {
		bom = f.BOM
	}

	return objects, bom, nil
}

// determinePrecedence decides which version to use based on keys and directives.
func determinePrecedence(key string, objectA, objectB scriptObject, options MergerOptions) (string, string) {
	// 1. Key List (Highest Priority)
	for _, k := range options.KeyList {
		if k == key {
			return "B", "keyList"
		}
	}

	// 2. Directives in A (Protect)
	if objectA.PreferSide == "A" {
		return "A", "directive"
	}
	if objectA.PreferSide == "B" {
		return "B", "directive"
	}

	// 3. Directives in B (Override)
	if objectB.PreferSide == "B" {
		return "B", "directive"
	}
	if objectB.PreferSide == "A" {
		return "A", "directive"
	}

	// 4. Default
	return "A", "default"
}

type internalMergeResult struct {
	Content           string
	EntriesAdded      []string
	EntriesRemoved    []string
	EntriesChanged    []string
	EntriesKept       []string
	ResolvedConflicts []ResolvedConflict
}

func (m *MergeService) performMerge(fileAPath, fileBPath string, options MergerOptions) (*internalMergeResult, error) {
	objectsA, bom, err := parseFileObjects(fileAPath)
	if err != nil {
		return nil, fmt.Errorf("error parsing file A: %w", err)
	}
	objectsB, _, err := parseFileObjects(fileBPath)
	if err != nil {
		return nil, fmt.Errorf("error parsing file B: %w", err)
	}

	lineEnding := "\r\n"

	mapB := make(map[string]scriptObject)
	for _, e := range objectsB {
		if e.Key != "" {
			mapB[e.Key] = e
		}
	}

	var output strings.Builder
	output.WriteString(bom)

	result := &internalMergeResult{}
	processedKeysInB := make(map[string]bool)

	// Iterate A
	for _, objA := range objectsA {
		// Handle loose comments/whitespace
		if objA.Key == "" {
			output.WriteString(objA.RawText)
			continue
		}

		key := objA.Key
		objB, existsInB := mapB[key]

		if existsInB {
			processedKeysInB[key] = true
			decision, reason := determinePrecedence(key, objA, objB, options)
			isDifferent := !areSemanticallyEqual(objA.ValueText, objB.ValueText)

			if decision == "B" {
				output.WriteString(objB.RawText)

				if isDifferent {
					result.EntriesChanged = append(result.EntriesChanged, key)
					result.ResolvedConflicts = append(result.ResolvedConflicts, ResolvedConflict{
						Key: key, UsedSide: "B", Reason: reason,
					})
				}
			} else {
				// Keep A
				output.WriteString(objA.RawText)

				if isDifferent {
					result.EntriesKept = append(result.EntriesKept, key)
					result.ResolvedConflicts = append(result.ResolvedConflicts, ResolvedConflict{
						Key: key, UsedSide: "A", Reason: reason,
					})
				} else {
					result.EntriesKept = append(result.EntriesKept, key)
				}
			}
		} else {
			// Only in A
			output.WriteString(objA.RawText)
			result.EntriesKept = append(result.EntriesKept, key)
		}
	}

	// Iterate Additions from B
	if options.AddAdditionalEntries {
		var newEntries []scriptObject

		// Iterate B to find new entries (preserving B's order)
		for _, objB := range objectsB {
			if objB.Key != "" && !processedKeysInB[objB.Key] {
				newEntries = append(newEntries, objB)
			}
		}

		if len(newEntries) > 0 {
			header := lineEnding + "############# Additional Entries From B (PDX-Merge-Tools) #############" + lineEnding
			output.WriteString(header)

			for _, objB := range newEntries {
				output.WriteString(objB.RawText)
				result.EntriesAdded = append(result.EntriesAdded, objB.Key)
			}
		}
	}

	result.Content = output.String()
	return result, nil
}

// WriteMergedFile writes content to outputPath as UTF-8 with BOM (no dialog). For merge editor save.
func (m *MergeService) WriteMergedFile(outputPath string, content string) error {
	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("error creating output directory: %w", err)
	}
	if !strings.HasPrefix(content, utf8BOM) {
		content = utf8BOM + content
	}
	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("error creating output file: %w", err)
	}
	defer file.Close()
	_, err = file.WriteString(content)
	return err
}

// GetMergeConflicts returns structured conflict chunks for the assisted merge editor.
func (m *MergeService) GetMergeConflicts(ctx context.Context, fileAPath, fileBPath string, options MergerOptions) ([]MergeConflictChunk, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	entitiesA, _, err := parseFileObjects(fileAPath)
	if err != nil {
		return nil, fmt.Errorf("error parsing file A: %w", err)
	}
	entitiesB, _, err := parseFileObjects(fileBPath)
	if err != nil {
		return nil, fmt.Errorf("error parsing file B: %w", err)
	}

	mapB := make(map[string]scriptObject)
	for _, e := range entitiesB {
		if e.Key != "" {
			mapB[e.Key] = e
		}
	}

	var chunks []MergeConflictChunk
	processedKeysInB := make(map[string]bool)

	for _, entA := range entitiesA {
		if entA.Key == "" {
			chunks = append(chunks, MergeConflictChunk{Type: "unchanged", Text: entA.RawText})
			continue
		}

		key := entA.Key
		entB, existsInB := mapB[key]

		if existsInB {
			processedKeysInB[key] = true
			if !areSemanticallyEqual(entA.ValueText, entB.ValueText) {
				chunks = append(chunks, MergeConflictChunk{Type: "conflict", Key: key, TextA: entA.RawText, TextB: entB.RawText})
			} else {
				chunks = append(chunks, MergeConflictChunk{Type: "unchanged", Text: entA.RawText})
			}
		} else {
			chunks = append(chunks, MergeConflictChunk{Type: "unchanged", Text: entA.RawText})
		}
	}

	if options.AddAdditionalEntries {
		var newEntries []scriptObject
		for _, entB := range entitiesB {
			if entB.Key != "" && !processedKeysInB[entB.Key] {
				newEntries = append(newEntries, entB)
			}
		}

		if len(newEntries) > 0 {
			chunks = append(chunks, MergeConflictChunk{Type: "unchanged", Text: "# Additional entries from B\n"})
			for _, entB := range newEntries {
				chunks = append(chunks, MergeConflictChunk{Type: "added", Text: entB.RawText})
			}
		}
	}

	return chunks, nil
}

// MergePreview returns a preview of what would be merged (no files written).
func (m *MergeService) MergePreview(ctx context.Context, pathsA, pathsB []string, outputDir string, options MergerOptions) ([]PreviewItem, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	filter := buildCollectFilter(options)
	filesA, err := m.FileService.CollectFilesFromPaths(pathsA, filter)
	if err != nil {
		return nil, fmt.Errorf("error collecting files from set A: %w", err)
	}
	filesB, err := m.FileService.CollectFilesFromPaths(pathsB, filter)
	if err != nil {
		return nil, fmt.Errorf("error collecting files from set B: %w", err)
	}
	matches, err := m.FileService.FindMatchingPaths(filesA, filesB, options.MatchByFilenameOnly)
	if err != nil {
		return nil, fmt.Errorf("error finding matching paths: %w", err)
	}
	var items []PreviewItem
	for relPath, match := range matches {
		outPath := outputPathWithSuffix(outputDir, relPath, options.OutputFileSuffix)
		_, overwrite := os.Stat(outPath)
		items = append(items, PreviewItem{
			RelPath:        relPath,
			PathA:          match.PathA,
			PathB:          match.PathB,
			OutputPath:     outPath,
			WouldOverwrite: overwrite == nil,
		})
	}
	return items, nil
}

// MergeMultipleFileSetsFiltered merges only the given relPaths (from preview). Pass nil/empty to merge all.
func (m *MergeService) MergeMultipleFileSetsFiltered(ctx context.Context, pathsA, pathsB []string, outputDir string, options MergerOptions, onlyRelPaths []string) ([]FileMergeResult, error) {
	return m.mergeMultipleFileSets(ctx, pathsA, pathsB, outputDir, options, onlyRelPaths)
}

func (m *MergeService) mergeMultipleFileSets(ctx context.Context, pathsA, pathsB []string, outputDir string, options MergerOptions, onlyRelPaths []string) ([]FileMergeResult, error) {
	// If the context is cancelled (from the frontend), return the error
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	filter := buildCollectFilter(options)
	filesA, err := m.FileService.CollectFilesFromPaths(pathsA, filter)
	if err != nil {
		return nil, fmt.Errorf("error collecting files from set A: %w", err)
	}
	filesB, err := m.FileService.CollectFilesFromPaths(pathsB, filter)
	if err != nil {
		return nil, fmt.Errorf("error collecting files from set B: %w", err)
	}
	matches, err := m.FileService.FindMatchingPaths(filesA, filesB, options.MatchByFilenameOnly)
	if err != nil {
		return nil, fmt.Errorf("error finding matching paths: %w", err)
	}

	// If the context is cancelled (from the frontend), return the error
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	include := make(map[string]bool)
	for _, r := range onlyRelPaths {
		include[r] = true
	}

	var results []FileMergeResult
	for relPath, match := range matches {
		if len(onlyRelPaths) > 0 && !include[relPath] {
			continue
		}
		// If the context is cancelled (from the frontend), return the error
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		mergeResult, err := m.performMerge(match.PathA, match.PathB, options)
		if err != nil {
			results = append(results, FileMergeResult{
				FilePath: relPath,
				Error:    err.Error(),
			})
			continue
		}
		changed := len(mergeResult.EntriesChanged)
		added := len(mergeResult.EntriesAdded)
		removed := len(mergeResult.EntriesRemoved)
		outPath := outputPathWithSuffix(outputDir, relPath, options.OutputFileSuffix)
		if err := m.WriteMergedFile(outPath, mergeResult.Content); err != nil {
			results = append(results, FileMergeResult{
				FilePath: relPath,
				Error:    err.Error(),
			})
			continue
		}
		results = append(results, FileMergeResult{
			FilePath:          relPath,
			FileAPath:         match.PathA,
			FileBPath:         match.PathB,
			OutputPath:        outPath,
			Changed:           changed,
			Added:             added,
			Removed:           removed,
			EntriesChanged:    mergeResult.EntriesChanged,
			EntriesAdded:      mergeResult.EntriesAdded,
			ResolvedConflicts: mergeResult.ResolvedConflicts,
		})
	}
	return results, nil
}

// MergePair is a user-specified file pair (JSON-safe for bindings)
type MergePair struct {
	PathA      string `json:"pathA"`
	PathB      string `json:"pathB"`
	OutputName string `json:"outputName"` // e.g. "merged_events.txt"; empty = use PathA basename
}

// MergePairs merges explicitly paired files. outputDir is the base output directory.
func (m *MergeService) MergePairs(ctx context.Context, pairs []MergePair, outputDir string, options MergerOptions) ([]FileMergeResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	var results []FileMergeResult
	for _, p := range pairs {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		mergeResult, err := m.performMerge(p.PathA, p.PathB, options)
		if err != nil {
			results = append(results, FileMergeResult{FilePath: p.PathA, Error: err.Error()})
			continue
		}
		outName := p.OutputName
		if outName == "" {
			outName = filepath.Base(p.PathA)
		}
		if filepath.Ext(outName) != ".txt" {
			outName = outName + ".txt"
		}
		outPath := filepath.Join(outputDir, outName)
		if err := m.WriteMergedFile(outPath, mergeResult.Content); err != nil {
			results = append(results, FileMergeResult{FilePath: p.PathA, Error: err.Error()})
			continue
		}
		results = append(results, FileMergeResult{
			FilePath:          p.PathA,
			FileAPath:         p.PathA,
			FileBPath:         p.PathB,
			OutputPath:        outPath,
			Changed:           len(mergeResult.EntriesChanged),
			Added:             len(mergeResult.EntriesAdded),
			Removed:           len(mergeResult.EntriesRemoved),
			EntriesChanged:    mergeResult.EntriesChanged,
			EntriesAdded:      mergeResult.EntriesAdded,
			ResolvedConflicts: mergeResult.ResolvedConflicts,
		})
	}
	return results, nil
}

// ValidationError describes a parse error in a merged file (JSON-safe for bindings)
type ValidationError struct {
	Path  string `json:"path"`
	Line  int    `json:"line"`
	Error string `json:"error"`
}

// ValidateMergedFiles runs the Paradox parser on each path and returns parse errors.
func (m *MergeService) ValidateMergedFiles(paths []string) []ValidationError {
	var errs []ValidationError
	for _, p := range paths {
		_, err := parser.ParseFile(p)
		if err != nil {
			line := 0
			if pe, ok := err.(interface{ Line() int }); ok {
				line = pe.Line()
			}
			errs = append(errs, ValidationError{Path: p, Line: line, Error: err.Error()})
		}
	}
	return errs
}

// GenerateMergeReport builds a Markdown report from merge results.
func (m *MergeService) GenerateMergeReport(results []FileMergeResult, totalAdded, totalChanged, totalRemoved int, labelA, labelB string) string {
	if labelA == "" {
		labelA = "A"
	}
	if labelB == "" {
		labelB = "B"
	}
	var b strings.Builder
	b.WriteString("# Merge Report\n\n")
	b.WriteString(fmt.Sprintf("**Summary:** %d files · +%d added · %d changed · -%d removed\n\n", len(results), totalAdded, totalChanged, totalRemoved))
	for _, r := range results {
		b.WriteString(fmt.Sprintf("## %s\n\n", r.FilePath))
		if r.Error != "" {
			b.WriteString(fmt.Sprintf("**Error:** %s\n\n", r.Error))
			continue
		}
		b.WriteString(fmt.Sprintf("**Stats:** +%d added, %d changed, -%d removed\n\n", r.Added, r.Changed, r.Removed))

		if len(r.EntriesAdded) > 0 {
			b.WriteString("### Added\n")
			for _, k := range r.EntriesAdded {
				b.WriteString(fmt.Sprintf("- %s\n", k))
			}
			b.WriteString("\n")
		}
		if len(r.EntriesChanged) > 0 {
			b.WriteString("### Changed\n")
			for _, k := range r.EntriesChanged {
				b.WriteString(fmt.Sprintf("- %s\n", k))
			}
			b.WriteString("\n")
		}

		if len(r.ResolvedConflicts) > 0 {
			b.WriteString("### Resolved Conflicts\n")
			for _, c := range r.ResolvedConflicts {
				side := c.UsedSide
				switch side {
				case "A":
					side = labelA
				case "B":
					side = labelB
				}
				b.WriteString(fmt.Sprintf("- **%s**: Used %s (%s)\n", c.Key, side, c.Reason))
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}
