package services

import (
	"fmt"
	"path/filepath"
	"sync/atomic"

	"paradox-modding-tools/services/internal/core"
)

// ############
// MergeService
// ############

// MergeService merges matching script files from two path sets into an output directory using core merge logic.
type MergeService struct {
	FileService *FileService
	cancelMerge atomic.Bool
}

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

// CancelMerge signals any running multi-file merge to stop. Partial results may be returned.
func (m *MergeService) CancelMerge() {
	m.cancelMerge.Store(true)
}

// MergeVanillaMod runs the full vanilla-vs-mod merge: resolves game script root, then merges into outputDir.
// Single backend call for the frontend. Supports cancellation via CancelMerge.
func (m *MergeService) MergeVanillaMod(game, installPath string, modPaths []string, outputDir string, options MergerOptions) ([]FileMergeResult, error) {
	if m.FileService == nil {
		return nil, fmt.Errorf("file service not configured")
	}
	root, err := m.FileService.GetGameScriptRoot(game, installPath)
	if err != nil {
		return nil, fmt.Errorf("game script root: %w", err)
	}
	return m.MergeMultipleFileSets([]string{root}, modPaths, outputDir, options)
}

// MergeMultipleFileSets merges matching files from pathsA and pathsB into outputDir.
// Uses core for discovery/matching and merge logic.
func (m *MergeService) MergeMultipleFileSets(pathsA, pathsB []string, outputDir string, options MergerOptions) ([]FileMergeResult, error) {
	if m.FileService == nil {
		return nil, fmt.Errorf("file service not configured")
	}
	filesA, err := m.FileService.CollectFilesFromPaths(pathsA, FileCollectorFilter{
		Extensions: []string{".txt"},
	})
	if err != nil {
		return nil, fmt.Errorf("error collecting files from set A: %w", err)
	}
	filesB, err := m.FileService.CollectFilesFromPaths(pathsB, FileCollectorFilter{
		Extensions: []string{".txt"},
	})
	if err != nil {
		return nil, fmt.Errorf("error collecting files from set B: %w", err)
	}
	matches, err := m.FileService.FindMatchingPaths(filesA, filesB)
	if err != nil {
		return nil, fmt.Errorf("error finding matching paths: %w", err)
	}

	coreOpts := core.MergeOptions{
		AddAdditionalEntries: options.AddAdditionalEntries,
		EntryPlacement:       options.EntryPlacement,
		KeyList:              options.KeyList,
		CustomCommentPrefix:  options.CustomCommentPrefix,
	}

	m.cancelMerge.Store(false)
	var results []FileMergeResult
	for relPath, match := range matches {
		if m.cancelMerge.Load() {
			break
		}
		mergedContent, mergeResult, err := core.GetMergedContent(match.PathA, match.PathB, coreOpts)
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
			FileAPath:  match.PathA,
			FileBPath:  match.PathB,
			OutputPath: outPath,
			Changed:    changed,
			Added:      added,
			Removed:    removed,
		})
	}
	return results, nil
}

// MergeTwoFiles merges two files and returns the merged content.
func (m *MergeService) MergeTwoFiles(fileAPath, fileBPath string, options MergerOptions) (string, error) {
	coreOpts := core.MergeOptions{
		AddAdditionalEntries: options.AddAdditionalEntries,
		EntryPlacement:       options.EntryPlacement,
		KeyList:              options.KeyList,
		CustomCommentPrefix:  options.CustomCommentPrefix,
	}
	content, _, err := core.GetMergedContent(fileAPath, fileBPath, coreOpts)
	return content, err
}

// MergeTwoFilesAndSave merges two files, opens the save dialog, and writes the result. Single backend call.
func (m *MergeService) MergeTwoFilesAndSave(fileAPath, fileBPath string, options MergerOptions) (string, error) {
	if m.FileService == nil {
		return "", fmt.Errorf("file service not configured")
	}
	content, err := m.MergeTwoFiles(fileAPath, fileBPath, options)
	if err != nil {
		return "", err
	}
	return m.FileService.SaveFile("Save merged file", "merged.txt", content)
}
