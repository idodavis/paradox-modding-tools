package services

type CompareService struct {
	fileService *FileService
}

func (c *CompareService) VanillaCompare(game string, vanillaInstallPath string, modPath string) (map[string]PathMatch, error) {
	gameScriptRoot, err := c.fileService.GetGameScriptRoot(game, vanillaInstallPath)
	if err != nil {
		return nil, err
	}
	return c.DirectoryCompare(gameScriptRoot, modPath)
}

func (c *CompareService) DirectoryCompare(setAPath string, setBPath string) (map[string]PathMatch, error) {
	filesA, err := c.fileService.CollectFilesFromPath(setAPath, FileCollectorFilter{
		Extensions: []string{".txt"},
	})
	if err != nil {
		return nil, err
	}
	filesB, err := c.fileService.CollectFilesFromPath(setBPath, FileCollectorFilter{
		Extensions: []string{".txt"},
	})
	if err != nil {
		return nil, err
	}
	matches, err := c.fileService.FindMatchingPaths(filesA, filesB, false)
	if err != nil {
		return nil, err
	}
	return matches, nil
}
