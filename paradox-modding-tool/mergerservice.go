package main

import (
	"fmt"
	"path/filepath"

	"paradox-modding-tool/internal/core"
)

// ############
// MergerService
// ############

// MergerService merges matching script files from two path sets into an output directory using core merge logic.
type MergerService struct{}

// MergerOptions configures how files are merged (JSON-safe for bindings)
type MergerOptions struct {
	AddAdditionalEntries bool     `json:"addAdditionalEntries"`
	EntryPlacement       string   `json:"entryPlacement"`
	KeyList              []string `json:"keyList"`
	CustomCommentPrefix  string   `json:"customCommentPrefix"`
}

// FileMergeResult is the result of merging one file (JSON-safe for bindings)
type FileMergeResult struct {
	FilePath   string `json:"filePath"`
	FileAPath  string `json:"fileAPath"`
	FileBPath  string `json:"fileBPath"`
	OutputPath string `json:"outputPath"`
	Changed    int    `json:"changed"`
	Added      int    `json:"added"`
	Removed    int    `json:"removed"`
	Error      string `json:"error,omitempty"`
}

// MergeMultipleFileSets merges matching files from pathsA and pathsB into outputDir.
// Uses core for discovery/matching and merge logic.
func (m *MergerService) MergeMultipleFileSets(pathsA, pathsB []string, outputDir string, options MergerOptions) ([]FileMergeResult, error) {
	filesA, err := core.CollectFromPaths(pathsA)
	if err != nil {
		return nil, fmt.Errorf("error collecting files from set A: %w", err)
	}
	filesB, err := core.CollectFromPaths(pathsB)
	if err != nil {
		return nil, fmt.Errorf("error collecting files from set B: %w", err)
	}
	matches := core.FindMatching(filesA, filesB)

	coreOpts := core.MergeOptions{
		AddAdditionalEntries: options.AddAdditionalEntries,
		EntryPlacement:       options.EntryPlacement,
		KeyList:              options.KeyList,
		CustomCommentPrefix:  options.CustomCommentPrefix,
	}

	var results []FileMergeResult
	for relPath, match := range matches {
		mergedContent, mergeResult, err := core.GetMergedContent(match.FileAPath, match.FileBPath, coreOpts)
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
		outPath := filepath.Join(outputDir, relPath)
		if err := core.WriteMergedFile(outPath, mergedContent); err != nil {
			results = append(results, FileMergeResult{
				FilePath: relPath,
				Error:    err.Error(),
			})
			continue
		}
		results = append(results, FileMergeResult{
			FilePath:   relPath,
			FileAPath:  match.FileAPath,
			FileBPath:  match.FileBPath,
			OutputPath: outPath,
			Changed:    changed,
			Added:      added,
			Removed:    removed,
		})
	}
	return results, nil
}

// MergeTwoFiles merges two files and returns the merged content. The frontend uses SaveFile to write it to a user-chosen path.
func (m *MergerService) MergeTwoFiles(fileAPath, fileBPath string, options MergerOptions) (string, error) {
	coreOpts := core.MergeOptions{
		AddAdditionalEntries: options.AddAdditionalEntries,
		EntryPlacement:       options.EntryPlacement,
		KeyList:              options.KeyList,
		CustomCommentPrefix:  options.CustomCommentPrefix,
	}
	content, _, err := core.GetMergedContent(fileAPath, fileBPath, coreOpts)
	return content, err
}
