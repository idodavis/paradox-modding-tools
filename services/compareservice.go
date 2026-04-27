package services

type CompareService struct {
	FileService *FileService
}

func (c *CompareService) VanillaCompare(game string, vanillaInstallPath string, modPath string) (map[string]PathMatch, error) {
	gameScriptRoot, err := c.FileService.GetGameScriptRoot(game, vanillaInstallPath)
	if err != nil {
		return nil, err
	}
	return c.DirectoryCompare(gameScriptRoot, modPath)
}

func (c *CompareService) DirectoryCompare(setAPath, setBPath string) (map[string]PathMatch, error) {
	return c.FileService.CollectAndMatchPaths(setAPath, setBPath, FileCollectorFilter{Extensions: []string{".txt"}}, false)
}
