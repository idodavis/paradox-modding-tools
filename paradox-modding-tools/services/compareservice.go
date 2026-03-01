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

func (c *CompareService) DirectoryCompare(setAPath, setBPath string) (map[string]PathMatch, error) {
	return c.fileService.CollectAndMatchPaths(setAPath, setBPath, FileCollectorFilter{Extensions: []string{".txt"}}, false)
}
