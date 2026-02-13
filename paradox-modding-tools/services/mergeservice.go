package services

import (
	"context"
	"fmt"
	"path/filepath"

	"paradox-modding-tools/services/internal/core"
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

// mergeTwoFiles merges two files and returns the merged content.
func (m *MergeService) mergeTwoFiles(ctx context.Context, fileAPath, fileBPath string, options MergerOptions) (string, error) {
	// If the context is cancelled (from the frontend), return the error
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	coreOpts := core.MergeOptions{
		AddAdditionalEntries: options.AddAdditionalEntries,
		EntryPlacement:       options.EntryPlacement,
		KeyList:              options.KeyList,
		CustomCommentPrefix:  options.CustomCommentPrefix,
	}
	content, _, err := core.GetMergedContent(fileAPath, fileBPath, coreOpts)
	return content, err
}

// MergeVanillaMod runs the full vanilla-vs-mod merge: resolves game script root, then merges into outputDir.
func (m *MergeService) MergeVanillaMod(ctx context.Context, game, installPath string, modPaths []string, outputDir string, options MergerOptions) ([]FileMergeResult, error) {
	// If the context is cancelled (from the frontend), return the error
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	root, err := m.FileService.GetGameScriptRoot(game, installPath)
	if err != nil {
		return nil, fmt.Errorf("game script root: %w", err)
	}
	return m.MergeMultipleFileSets(ctx, []string{root}, modPaths, outputDir, options)
}

// MergeMultipleFileSets merges matching files from pathsA and pathsB into outputDir.
// Uses core for discovery/matching and merge logic.
func (m *MergeService) MergeMultipleFileSets(ctx context.Context, pathsA, pathsB []string, outputDir string, options MergerOptions) ([]FileMergeResult, error) {
	// If the context is cancelled (from the frontend), return the error
	if ctx.Err() != nil {
		return nil, ctx.Err()
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

	// If the context is cancelled (from the frontend), return the error
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	coreOpts := core.MergeOptions{
		AddAdditionalEntries: options.AddAdditionalEntries,
		EntryPlacement:       options.EntryPlacement,
		KeyList:              options.KeyList,
		CustomCommentPrefix:  options.CustomCommentPrefix,
	}

	var results []FileMergeResult
	for relPath, match := range matches {
		// If the context is cancelled (from the frontend), return the error
		if ctx.Err() != nil {
			return nil, ctx.Err()
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

// MergeTwoFilesAndSave merges two files, opens the save dialog, and writes the result. Single backend call.
func (m *MergeService) MergeTwoFilesAndSave(ctx context.Context, fileAPath, fileBPath string, options MergerOptions) (string, error) {
	// If the context is cancelled (from the frontend), return the error
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	content, err := m.mergeTwoFiles(ctx, fileAPath, fileBPath, options)
	if err != nil {
		return "", err
	}
	return m.FileService.SaveFile("Save merged file", "merged.txt", content, ".txt")
}
