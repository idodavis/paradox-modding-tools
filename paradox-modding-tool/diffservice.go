package main

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/sergi/go-diff/diffmatchpatch"
)

// DiffService provides diff functionality for comparing files
type DiffService struct{}

// DiffLine represents a single line in a diff
type DiffLine struct {
	Type       string `json:"type"`       // "header", "add", "remove", "context", "other"
	Content    string `json:"content"`    // The line content
	OldLineNum *int   `json:"oldLineNum"` // Line number in old file (nil if not applicable)
	NewLineNum *int   `json:"newLineNum"` // Line number in new file (nil if not applicable)
}

// normalizeLineEndings converts \r\n and \r to \n so both sides compare consistently.
func normalizeLineEndings(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

// GetDiff returns a structured diff between two files (DiffLine slice with type, content, line numbers).
// Uses line-based diff to avoid cascading false positives when only a block of lines changes (e.g. removing comments at the top).
func (d *DiffService) GetDiff(beforeFilePath string, afterFilePath string) ([]DiffLine, error) {
	beforeContent, err := os.ReadFile(beforeFilePath)
	if err != nil {
		return nil, fmt.Errorf("error reading file A: %w", err)
	}
	afterContent, err := os.ReadFile(afterFilePath)
	if err != nil {
		return nil, fmt.Errorf("error reading file B: %w", err)
	}
	before := normalizeLineEndings(string(beforeContent))
	after := normalizeLineEndings(string(afterContent))

	dmp := diffmatchpatch.New()
	line1, line2, lineArray := dmp.DiffLinesToChars(before, after)
	diffs := dmp.DiffMain(line1, line2, false)
	diffs = dmp.DiffCharsToLines(diffs, lineArray)

	const contextLines = 5
	diffText := lineDiffsToUnified(beforeFilePath, afterFilePath, diffs, contextLines)
	return parseDiffToLines(diffText), nil
}

// lineDiffsToUnified converts sergi []Diff (from line-based diff) into unified diff text.
func lineDiffsToUnified(fromName, toName string, diffs []diffmatchpatch.Diff, contextLines int) string {
	if len(diffs) == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString("--- " + fromName + "\n")
	out.WriteString("+++ " + toName + "\n")

	// Flatten diffs into (kind, line) pairs so we can build hunks with context.
	type lineOp struct {
		kind int // -1 delete, 0 equal, 1 insert
		line string
	}
	var ops []lineOp
	for _, d := range diffs {
		kind := 0
		switch d.Type {
		case diffmatchpatch.DiffDelete:
			kind = -1
		case diffmatchpatch.DiffInsert:
			kind = 1
		}
		lines := strings.Split(d.Text, "\n")
		for i, ln := range lines {
			// Skip the last empty element if the text ends with \n
			if i == len(lines)-1 && ln == "" {
				continue
			}
			// Add \n to all lines except the very last one (if it didn't originally end with \n)
			if i < len(lines)-1 {
				ops = append(ops, lineOp{kind, ln + "\n"})
			} else {
				// Last line - only add \n if original text had it
				if strings.HasSuffix(d.Text, "\n") {
					ops = append(ops, lineOp{kind, ln + "\n"})
				} else {
					ops = append(ops, lineOp{kind, ln})
				}
			}
		}
	}

	// Precompute old/new line numbers at each op index.
	oldN := make([]int, len(ops)+1)
	newN := make([]int, len(ops)+1)
	oldN[0], newN[0] = 1, 1
	for idx, op := range ops {
		if op.kind != 1 {
			oldN[idx+1] = oldN[idx] + 1
		} else {
			oldN[idx+1] = oldN[idx]
		}
		if op.kind != -1 {
			newN[idx+1] = newN[idx] + 1
		} else {
			newN[idx+1] = newN[idx]
		}
	}

	i := 0
	for i < len(ops) {
		// Find next change (delete or insert)
		start := i
		for start < len(ops) && ops[start].kind == 0 {
			start++
		}
		if start >= len(ops) {
			break
		}
		// Find end of this change region (run of deletes/inserts, with optional context gaps we’ll include in one hunk if within 2*context)
		end := start
		lastChange := start
		for end < len(ops) {
			if ops[end].kind != 0 {
				lastChange = end
			}
			end++
			if end < len(ops) && ops[end].kind == 0 {
				// look ahead: if context is small, keep going
				gap := 0
				for gap < end && end+gap < len(ops) && ops[end+gap].kind == 0 {
					gap++
				}
				if gap > contextLines*2 {
					break
				}
			}
		}
		end = lastChange + 1

		ctxStart := start - contextLines
		if ctxStart < 0 {
			ctxStart = 0
		}
		ctxEnd := end + contextLines
		if ctxEnd > len(ops) {
			ctxEnd = len(ops)
		}
		oldCount := 0
		newCount := 0
		for j := ctxStart; j < ctxEnd; j++ {
			if ops[j].kind != 1 {
				oldCount++
			}
			if ops[j].kind != -1 {
				newCount++
			}
		}
		out.WriteString(fmt.Sprintf("@@ -%d,%d +%d,%d @@\n", oldN[ctxStart], oldCount, newN[ctxStart], newCount))
		for j := ctxStart; j < ctxEnd; j++ {
			switch ops[j].kind {
			case -1:
				out.WriteString("-" + ops[j].line)
			case 1:
				out.WriteString("+" + ops[j].line)
			default:
				out.WriteString(" " + ops[j].line)
			}
		}
		i = ctxEnd
	}
	return out.String()
}

// parseDiffToLines parses a unified diff into structured DiffLine objects
// Line numbers correspond to actual line numbers in the source files
func parseDiffToLines(diffText string) []DiffLine {
	if diffText == "" {
		return []DiffLine{}
	}

	lines := strings.Split(diffText, "\n")
	result := []DiffLine{}
	var oldLineNum *int
	var newLineNum *int

	// Regex to match @@ headers: @@ -oldStart,oldCount +newStart,newCount @@
	headerRegex := regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

	for _, line := range lines {
		// Parse @@ headers to get line number ranges
		if strings.HasPrefix(line, "@@") {
			matches := headerRegex.FindStringSubmatch(line)
			if len(matches) >= 4 {
				oldStart, _ := strconv.Atoi(matches[1])
				newStart, _ := strconv.Atoi(matches[3])
				oldLineNum = &oldStart
				newLineNum = &newStart
			}
			result = append(result, DiffLine{
				Type:       "header",
				Content:    line,
				OldLineNum: nil,
				NewLineNum: nil,
			})
			continue
		}

		// Handle file headers
		if strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++") {
			result = append(result, DiffLine{
				Type:       "header",
				Content:    line,
				OldLineNum: nil,
				NewLineNum: nil,
			})
			continue
		}

		// Handle diff lines
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			// Addition - use new file line number
			var newNum *int
			if newLineNum != nil {
				num := *newLineNum
				newNum = &num
				*newLineNum++
			}
			result = append(result, DiffLine{
				Type:       "add",
				Content:    line[1:], // Remove the '+' prefix
				OldLineNum: nil,
				NewLineNum: newNum,
			})
		} else if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			// Deletion - use old file line number
			var oldNum *int
			if oldLineNum != nil {
				num := *oldLineNum
				oldNum = &num
				*oldLineNum++
			}
			result = append(result, DiffLine{
				Type:       "remove",
				Content:    line[1:], // Remove the '-' prefix
				OldLineNum: oldNum,
				NewLineNum: nil,
			})
		} else if strings.HasPrefix(line, " ") {
			// Context line - increment both
			var oldNum *int
			var newNum *int
			if oldLineNum != nil {
				num := *oldLineNum
				oldNum = &num
				*oldLineNum++
			}
			if newLineNum != nil {
				num := *newLineNum
				newNum = &num
				*newLineNum++
			}
			result = append(result, DiffLine{
				Type:       "context",
				Content:    line[1:], // Remove the ' ' prefix
				OldLineNum: oldNum,
				NewLineNum: newNum,
			})
		} else if line != "" {
			// Other lines (non-empty, non-standard lines)
			result = append(result, DiffLine{
				Type:       "other",
				Content:    line,
				OldLineNum: nil,
				NewLineNum: nil,
			})
		}
	}

	return result
}
