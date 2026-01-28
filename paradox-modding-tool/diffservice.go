package main

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/aymanbagabas/go-udiff"
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

// GetDiff returns a structured diff between two files (DiffLine slice with type, content, line numbers).
func (d *DiffService) GetDiff(beforeFilePath string, afterFilePath string) ([]DiffLine, error) {
	beforeContent, err := os.ReadFile(beforeFilePath)
	if err != nil {
		return nil, fmt.Errorf("error reading file A: %w", err)
	}
	afterContent, err := os.ReadFile(afterFilePath)
	if err != nil {
		return nil, fmt.Errorf("error reading file B: %w", err)
	}
	before := string(beforeContent)
	after := string(afterContent)
	edits := udiff.Strings(before, after)
	const contextLines = 5
	diffText, err := udiff.ToUnified(beforeFilePath, afterFilePath, before, edits, contextLines)
	if err != nil {
		diffText = udiff.Unified(beforeFilePath, afterFilePath, before, after)
	}
	return parseDiffToLines(diffText), nil
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
		} else {
			// Other lines (empty, etc.)
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
