package services

type CompareService struct {
	fileService *FileService
}

func (c *CompareService) VanillaCompare(game string, vanillaInstallPath string, modPaths []string) (map[string]PathMatch, error) {
	gameScriptRoot, err := c.fileService.GetGameScriptRoot(game, vanillaInstallPath)
	if err != nil {
		return nil, err
	}
	vanillaFiles, err := c.fileService.CollectFilesFromPaths([]string{gameScriptRoot}, FileCollectorFilter{
		Extensions: []string{".txt"},
	})
	if err != nil {
		return nil, err
	}
	modFiles, err := c.fileService.CollectFilesFromPaths(modPaths, FileCollectorFilter{
		Extensions: []string{".txt"},
	})
	if err != nil {
		return nil, err
	}
	matches, err := c.fileService.FindMatchingPaths(vanillaFiles, modFiles)
	if err != nil {
		return nil, err
	}
	return matches, nil
}

func (c *CompareService) TwoSetsCompare(setA []string, setB []string) (map[string]PathMatch, error) {
	filesA, err := c.fileService.CollectFilesFromPaths(setA, FileCollectorFilter{
		Extensions: []string{".txt"},
	})
	if err != nil {
		return nil, err
	}
	filesB, err := c.fileService.CollectFilesFromPaths(setB, FileCollectorFilter{
		Extensions: []string{".txt"},
	})
	if err != nil {
		return nil, err
	}
	matches, err := c.fileService.FindMatchingPaths(filesA, filesB)
	if err != nil {
		return nil, err
	}
	return matches, nil
}
