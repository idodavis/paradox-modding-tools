package services

type CompareService struct {
	fileService *FileService
}

func (c *CompareService) VanillaCompare(game string, vanillaInstallPath string, modPaths []string) (map[string]PathMatch, error) {
	gameScriptRoot, err := c.fileService.GetGameScriptRoot(game, vanillaInstallPath)
	if err != nil {
		return nil, err
	}
	vanillaFiles, err := c.fileService.CollectFilesFromPaths([]string{gameScriptRoot})
	if err != nil {
		return nil, err
	}
	modFiles, err := c.fileService.CollectFilesFromPaths(modPaths)
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
	filesA, err := c.fileService.CollectFilesFromPaths(setA)
	if err != nil {
		return nil, err
	}
	filesB, err := c.fileService.CollectFilesFromPaths(setB)
	if err != nil {
		return nil, err
	}
	matches, err := c.fileService.FindMatchingPaths(filesA, filesB)
	if err != nil {
		return nil, err
	}
	return matches, nil
}
