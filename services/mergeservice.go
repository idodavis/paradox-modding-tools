package services

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal"
	parser "paradox-modding-tools/services/internal/interpreter"
)

const (
	additionalEntriesHdr = "\n############# Additional Entries From B (PDX-Merge-Tools) #############\n"
)

var precedenceRe = regexp.MustCompile(`(?i)(?:PREFER|USE|MERGE)\s*:\s*([AB])|(?i)(?:PROTECT|KEEP)`)

// MergeService merges matching script files from two path sets into an output directory using core merge logic.
type MergeService struct {
	FileService *FileService
}

// MergerOptions configures how files are merged.
type MergerOptions struct {
	AddAdditionalEntries     bool     `json:"addAdditionalEntries"`
	ManualConflictResolution bool     `json:"manualConflictResolution"`
	KeyList                  []string `json:"keyList"`
	MatchByFilenameOnly      bool     `json:"matchByFilenameOnly"`
	IncludePathPattern       string   `json:"includePathPattern"`
	ExcludePathPattern       string   `json:"excludePathPattern"`
	OutputFileSuffix         string   `json:"outputFileSuffix"` // e.g. "_merged" meaning: events/foo.txt -> events/foo_merged.txt
	OutputDir                string   `json:"outputDir"`
}

// PreviewItem is a single file match for the merge preview.
type PreviewItem struct {
	RelPath        string `json:"relPath"`
	PathA          string `json:"pathA"`
	PathB          string `json:"pathB"`
	OutputPath     string `json:"outputPath"`
	WouldOverwrite bool   `json:"wouldOverwrite"`
}

// FileMergeResult is the result of merging one file.
type FileMergeResult struct {
	FilePath          string             `json:"filePath"`
	FileAPath         string             `json:"fileAPath"`
	FileBPath         string             `json:"fileBPath"`
	OutputPath        string             `json:"outputPath"`
	Changed           int                `json:"changed"`
	Added             int                `json:"added"`
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

// MergeConflictChunk is a unit of content for the assisted merge editor (and internal merge iteration).
// ObjA/ObjB are internal-only; JSON output omits them.
type MergeConflictChunk struct {
	Type       string        `json:"type"` // "unchanged", "added", or "conflict"
	TextA      string        `json:"textA"`
	TextB      string        `json:"textB"`
	StartLineA int           `json:"startLineA"`
	StartLineB int           `json:"startLineB"`
	EndLineA   int           `json:"endLineA"`
	EndLineB   int           `json:"endLineB"`
	ObjA       *scriptObject `json:"-"`
	ObjB       *scriptObject `json:"-"`
}

// MergePair is a user-specified file pair for explicit merging.
type MergePair struct {
	PathA      string `json:"pathA"`
	PathB      string `json:"pathB"`
	OutputName string `json:"outputName"` // e.g. "merged_events.txt"; empty = use PathA basename
}

// ValidationError describes a parse error in a merged file.
type ValidationError struct {
	Path  string `json:"path"`
	Line  int    `json:"line"`
	Error string `json:"error"`
}

// scriptObject represents a parsed top-level entry (assignment or object) with its comments.
type scriptObject struct {
	Key        string
	RawText    string   // Full text including comments
	ValueText  string   // Just the value part (for normalization)
	Comments   []string // Preceding comments
	PreferSide string   // "A" or "B" parsed from directives
	StartLine  int
	EndLine    int
}

// mergeResult holds the result of merging one file (internal use).
type mergeResult struct {
	Content           string
	EntriesAdded      []string
	EntriesChanged    []string
	ResolvedConflicts []ResolvedConflict
}

// MergePreview collects matching files from pathA/pathB and returns preview items with output paths.
func (m *MergeService) MergePreview(ctx context.Context, pathA, pathB, outputDir string, opts MergerOptions) ([]PreviewItem, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	matches, err := m.FileService.CollectAndMatchPaths(pathA, pathB, FileCollectorFilter{
		Extensions:  []string{".txt"},
		IncludePath: opts.IncludePathPattern,
		ExcludePath: opts.ExcludePathPattern,
	}, opts.MatchByFilenameOnly)
	if err != nil {
		return nil, err
	}
	var items []PreviewItem
	for relPath, match := range matches {
		outPath := outputPathWithSuffix(outputDir, relPath, opts.OutputFileSuffix)
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

// Merge performs the merge for each task (PreviewItem) and writes results. Single entry point for merge operations.
func (m *MergeService) Merge(ctx context.Context, tasks []PreviewItem, opts MergerOptions) ([]FileMergeResult, error) {
	var results []FileMergeResult
	for _, task := range tasks {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		results = append(results, m.mergeAndWrite(task.PathA, task.PathB, task.OutputPath, task.RelPath, opts))
	}
	return results, nil
}

// GetMergeConflicts returns structured conflict chunks for the assisted merge editor.
func (m *MergeService) GetMergeConflicts(ctx context.Context, fileAPath, fileBPath string, options MergerOptions) ([]MergeConflictChunk, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	items, _, err := m.mergeFileItems(fileAPath, fileBPath, options)
	if err != nil {
		return nil, err
	}
	return consolidateChunks(items), nil
}

// ValidateMergedFiles runs the Paradox parser on each path and returns parse errors.
func (m *MergeService) ValidateMergedFiles(paths []string) []ValidationError {
	results := parser.ValidatePaths(paths)
	errs := make([]ValidationError, len(results))
	for i, r := range results {
		errs[i] = ValidationError{Path: r.Path, Line: r.Line, Error: r.Error}
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
	sideLabel := map[string]string{"A": labelA, "B": labelB}
	var b strings.Builder
	fmt.Fprintf(&b, "# Merge Report\n\n**Summary:** %d files · +%d added · %d changed · -%d removed\n\n", len(results), totalAdded, totalChanged, totalRemoved)
	for _, r := range results {
		fmt.Fprintf(&b, "## %s\n\n", r.FilePath)
		if r.Error != "" {
			fmt.Fprintf(&b, "**Error:** %s\n\n", r.Error)
			continue
		}
		fmt.Fprintf(&b, "**Stats:** +%d added, %d changed\n\n", r.Added, r.Changed)
		for _, sec := range []struct {
			title string
			items []string
		}{{"Added", r.EntriesAdded}, {"Changed", r.EntriesChanged}} {
			if len(sec.items) == 0 {
				continue
			}
			b.WriteString("### " + sec.title + "\n")
			for _, k := range sec.items {
				b.WriteString("- " + k + "\n")
			}
			b.WriteString("\n")
		}
		if len(r.ResolvedConflicts) > 0 {
			b.WriteString("### Resolved Conflicts\n")
			for _, c := range r.ResolvedConflicts {
				side := sideLabel[c.UsedSide]
				if side == "" {
					side = c.UsedSide
				}
				fmt.Fprintf(&b, "- **%s**: Used %s (%s)\n", c.Key, side, c.Reason)
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

func outputPathWithSuffix(outputDir, relPath, suffix string) string {
	if suffix == "" {
		return filepath.Join(outputDir, relPath)
	}
	ext := filepath.Ext(relPath)
	return filepath.Join(outputDir, strings.TrimSuffix(relPath, ext)+suffix+".txt")
}

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

func parseFileObjects(path string) ([]scriptObject, string, error) {
	f, err := parser.ParseFile(path)
	if err != nil {
		return nil, "", err
	}

	var objects []scriptObject
	var pendingPreamble strings.Builder
	var pendingComments []string

	lineEnding := "\n"
	currentLine := 1
	pendingStartLine := 1

	for _, entry := range f.Entries {
		rawText := entry.GetRawText()
		if lineEnding == "\n" && strings.Contains(rawText, "\r\n") {
			lineEnding = "\r\n"
		}

		pendingPreamble.WriteString(rawText)

		lineDelta := strings.Count(rawText, "\n")
		inclusiveEndLine := currentLine + lineDelta

		if c := entry.Comment; c != "" {
			pendingComments = append(pendingComments, c)
		}

		if expr := entry.Expression; expr != nil && expr.Key != "" {
			prefer := ""
			for _, pc := range pendingComments {
				if p := parsePrecedenceFromComment(pc); p != "" {
					prefer = p
					break
				}
			}

			objects = append(objects, scriptObject{
				Key:        expr.Key,
				RawText:    pendingPreamble.String(),
				ValueText:  expr.GetRawText(),
				Comments:   pendingComments,
				PreferSide: prefer,
				StartLine:  pendingStartLine,
				EndLine:    inclusiveEndLine,
			})

			pendingPreamble.Reset()
			pendingComments = nil
			pendingStartLine = inclusiveEndLine + 1
		}

		currentLine += lineDelta
	}

	if pendingPreamble.Len() > 0 {
		objects = append(objects, scriptObject{
			RawText:   pendingPreamble.String(),
			StartLine: pendingStartLine,
			EndLine:   currentLine,
		})
	}

	bom := utf8BOM
	if f.BOM != "" {
		bom = f.BOM
	}
	return objects, bom, nil
}

func determinePrecedence(key string, a, b scriptObject, opts MergerOptions) (string, string) {
	if slices.Contains(opts.KeyList, key) {
		return "B", "keyList"
	}
	for _, obj := range []scriptObject{a, b} {
		if obj.PreferSide != "" {
			return obj.PreferSide, "directive"
		}
	}
	return "A", "default"
}

// mergeFileItems builds conflict chunks by comparing parsed objects from fileA and fileB.
func (m *MergeService) mergeFileItems(fileAPath, fileBPath string, opts MergerOptions) ([]MergeConflictChunk, string, error) {
	objectsA, bom, err := parseFileObjects(fileAPath)
	if err != nil {
		return nil, "", fmt.Errorf("parsing file A: %w", err)
	}
	objectsB, _, err := parseFileObjects(fileBPath)
	if err != nil {
		return nil, "", fmt.Errorf("parsing file B: %w", err)
	}
	mapB := make(map[string]scriptObject, len(objectsB))
	for _, e := range objectsB {
		if e.Key != "" {
			mapB[e.Key] = e
		}
	}
	keysInA := make(map[string]bool, len(objectsA))
	var items []MergeConflictChunk

	for _, entA := range objectsA {
		a := entA
		chunk := MergeConflictChunk{
			Type: "unchanged", TextA: a.RawText,
			StartLineA: a.StartLine, EndLineA: a.EndLine, ObjA: &a,
		}
		if a.Key != "" {
			keysInA[a.Key] = true
			if entB, ok := mapB[a.Key]; ok {
				b := entB
				chunk.StartLineB, chunk.EndLineB, chunk.ObjB = b.StartLine, b.EndLine, &b
				if !internal.ScriptValuesEqual(a.ValueText, b.ValueText) {
					chunk.Type, chunk.TextB = "conflict", b.RawText
				}
			}
		}
		items = append(items, chunk)
	}
	if opts.AddAdditionalEntries {
		for _, entB := range objectsB {
			if entB.Key != "" && !keysInA[entB.Key] {
				b := entB
				items = append(items, MergeConflictChunk{
					Type: "added", TextB: b.RawText,
					StartLineB: b.StartLine, EndLineB: b.EndLine, ObjB: &b,
				})
			}
		}
	}
	return items, bom, nil
}

// performMerge resolves conflicts using precedence rules and produces final content.
func (m *MergeService) performMerge(fileAPath, fileBPath string, opts MergerOptions) (*mergeResult, error) {
	items, bom, err := m.mergeFileItems(fileAPath, fileBPath, opts)
	if err != nil {
		return nil, err
	}
	var out strings.Builder
	out.WriteString(bom)
	r := &mergeResult{}
	addHeader := false

	for _, it := range items {
		switch it.Type {
		case "unchanged":
			out.WriteString(it.TextA)
			continue
		case "added":
			if !addHeader {
				out.WriteString(additionalEntriesHdr)
				addHeader = true
			}
			out.WriteString(it.TextB)
			r.EntriesAdded = append(r.EntriesAdded, it.ObjB.Key)
			continue
		}
		// conflict
		decision, reason := determinePrecedence(it.ObjA.Key, *it.ObjA, *it.ObjB, opts)
		if decision == "B" {
			out.WriteString(it.TextB)
			r.EntriesChanged = append(r.EntriesChanged, it.ObjA.Key)
		} else {
			out.WriteString(it.TextA)
		}
		r.ResolvedConflicts = append(r.ResolvedConflicts, ResolvedConflict{
			Key: it.ObjA.Key, UsedSide: decision, Reason: reason,
		})
	}
	r.Content = out.String()
	return r, nil
}

// consolidateChunks merges consecutive same-type non-conflict chunks into single chunks.
func consolidateChunks(chunks []MergeConflictChunk) []MergeConflictChunk {
	result := make([]MergeConflictChunk, 0, len(chunks))
	for _, c := range chunks {
		if len(result) > 0 {
			last := &result[len(result)-1]
			if last.Type == c.Type && c.Type == "unchanged" {
				last.TextA += c.TextA
				last.TextB += c.TextB
				last.EndLineA = c.EndLineA
				last.EndLineB = c.EndLineB
				continue
			}
		}
		result = append(result, c)
	}
	return result
}

// mergeAndWrite performs merge and writes the result to outputPath.
func (m *MergeService) mergeAndWrite(pathA, pathB, outputPath, filePath string, opts MergerOptions) FileMergeResult {
	mr, err := m.performMerge(pathA, pathB, opts)
	if err != nil {
		return FileMergeResult{FilePath: filePath, Error: err.Error()}
	}
	if err := m.FileService.WriteWithBOM(outputPath, mr.Content); err != nil {
		return FileMergeResult{FilePath: filePath, Error: err.Error()}
	}
	return FileMergeResult{
		FilePath: filePath, FileAPath: pathA, FileBPath: pathB, OutputPath: outputPath,
		Changed: len(mr.EntriesChanged), Added: len(mr.EntriesAdded),
		EntriesChanged: mr.EntriesChanged, EntriesAdded: mr.EntriesAdded, ResolvedConflicts: mr.ResolvedConflicts,
	}
}
