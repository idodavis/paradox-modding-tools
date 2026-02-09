package services

const (
	// Steam app IDs (used for SteamDB PatchnotesRSS)
	steamAppIDCK3       = "1158310"
	steamAppIDEU5       = "3450310"
	wikiUrlCK3          = "https://ck3.paradoxwikis.com/Modding"
	wikiUrlEU5          = "https://eu5.paradoxwikis.com/Modding"
	scriptRootFolderCK3 = "game"
	scriptRootFolderEU5 = "game/in_game"
	docFileCK3          = ".info"
	docFileEU5          = "readme.txt"
)

type ConstantsService struct{}

type AppConstants struct {
	CK3 CK3Constants `json:"ck3"`
	EU5 EU5Constants `json:"eu5"`
}

type CK3Constants struct {
	SteamAppID       string `json:"steamAppId"`
	WikiUrl          string `json:"wikiUrl"`
	ScriptRootFolder string `json:"scriptRootFolder"`
	DocFile          string `json:"docFileName"`
}

type EU5Constants struct {
	SteamAppID       string `json:"steamAppId"`
	WikiUrl          string `json:"wikiUrl"`
	ScriptRootFolder string `json:"scriptRootFolder"`
	DocFile          string `json:"docFileName"`
}

func (c *ConstantsService) GetAppConstants() AppConstants {
	return AppConstants{
		CK3: CK3Constants{
			SteamAppID:       steamAppIDCK3,
			WikiUrl:          wikiUrlCK3,
			ScriptRootFolder: scriptRootFolderCK3,
			DocFile:          docFileCK3,
		},
		EU5: EU5Constants{
			SteamAppID:       steamAppIDEU5,
			WikiUrl:          wikiUrlEU5,
			ScriptRootFolder: scriptRootFolderEU5,
			DocFile:          docFileEU5,
		},
	}
}
