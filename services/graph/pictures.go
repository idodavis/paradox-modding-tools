// pictures.go finds the picture and the painting a definition shows.

package graph

import (
	"path"
	"slices"
	"strings"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/internal/texture"
	"paradox-modding-tools/services/workspace"
)

// pictureRow is how one game's database shows a picture: the file a
// definition names, or the file named after it.
type pictureRow struct {
	Game     string
	Database string

	// Painting marks the painting behind a definition's head, not its icon.
	// Of a database's rows for either, the first that leads to a file shows.
	Painting bool

	// Key is the keys from the definition to the one that writes the file,
	// joined by slashes. A constant, as @icon, stands for its text, and a
	// value that does not start with gfx/ is a path under Folder. It is ""
	// for a file named after the definition.
	Key    string
	Folder string

	// Pattern is the path of a file named after the definition, with %s for
	// its name, or for the value of Field where that is set.
	Pattern string
	Field   string

	// Silhouette is set where the game tints the picture in the text's color.
	Silhouette bool
}

// patternName stands for a name in a row's pattern.
const patternName = "%s"

// pictureRows are the pictures of each game's goods, buildings, technologies
// and pop types. Each cites the game's file and line, or the files' folder.
var pictureRows = []pictureRow{
	// gfx/interface/icons/trade/trade_goods/trade_good_alum.dds and
	// trade_good_categories/trade_category_agricultural_goods.dds
	{Game: game.CK3, Database: "trade/trade_good_types", Pattern: "gfx/interface/icons/trade/trade_goods/%s"},
	{Game: game.CK3, Database: "trade/trade_good_categories", Pattern: "gfx/interface/icons/trade/trade_good_categories/%s"},

	// regular_district_buildings/00_guild_district_buildings.txt:852 writes
	// type_icon, a shape the game tints
	{Game: game.CK3, Database: "buildings", Key: "type_icon", Folder: "gfx/interface/icons/building_types", Silhouette: true},

	// common/culture/innovations/00_cultural_maa_innovations.txt:2 writes
	// the constant that line 40 gives as the icon
	{Game: game.CK3, Database: "culture/innovations", Key: "icon"},

	// common/districts/duchy_capital_districts.txt:8 and 16 write a
	// background for some cases and one for the rest. An era's is
	// gfx/interface/window_culture/culture_era_illustrations/
	// culture_era_tribal.dds
	{Game: game.CK3, Database: "districts", Painting: true, Key: "background/reference"},
	{Game: game.CK3, Database: "culture/eras", Painting: true, Pattern: "gfx/interface/window_culture/culture_era_illustrations/%s"},

	// common/goods/00_goods.txt:10; buildings/01_industry.txt:175 and 211, an
	// icon and the background behind it;
	// technology/technologies/10_production.txt:6-7, whose category names
	// gfx/interface/illustrations/tech_tree/illustration_tech_production.dds
	{Game: game.Vic3, Database: "goods", Key: "texture"},
	{Game: game.Vic3, Database: "buildings", Key: "icon"},
	{Game: game.Vic3, Database: "buildings", Painting: true, Key: "background"},
	{Game: game.Vic3, Database: "technology/technologies", Key: "texture"},
	{
		Game: game.Vic3, Database: "technology/technologies", Painting: true, Field: "category",
		Pattern: "gfx/interface/illustrations/tech_tree/illustration_tech_%s",
	},

	// common/pop_types/laborers.txt:2 writes a pop type's icon, as
	// gfx/interface/icons/pops_icons/laborers.dds
	{Game: game.Vic3, Database: "pop_types", Key: "texture"},

	// common/production_methods/01_industry.txt:928 writes a production
	// method's icon, and common/production_method_groups/01_industry.txt:95
	// its group's
	{Game: game.Vic3, Database: "production_methods", Key: "texture"},
	{Game: game.Vic3, Database: "production_method_groups", Key: "texture"},

	// main_menu/gfx/interface/icons/trade_goods/icon_goods_alum.dds,
	// icons/buildings/ablaq_palace.dds, icons/building_categories/
	// consumer_goods_category.dds, icons/pops/clergy.dds,
	// advance/age_1_traditions.dds is an advance's icon, and
	// illustrations/advances/age_1_traditions.dds an age's painting
	{Game: game.EU5, Database: "goods", Pattern: "gfx/interface/icons/trade_goods/icon_goods_%s"},
	{Game: game.EU5, Database: "pop_types", Pattern: "gfx/interface/icons/pops/%s"},
	{Game: game.EU5, Database: "building_types", Pattern: "gfx/interface/icons/buildings/%s"},
	{Game: game.EU5, Database: "building_categories", Pattern: "gfx/interface/icons/building_categories/%s"},

	// in_game/common/advances/0_age_of_renaissance.txt:6 writes an advance's
	// icon. One that writes none shows the file named after it, as
	// advance/baptism_books.dds, else advance/_default.dds
	{Game: game.EU5, Database: "advances", Key: "icon", Folder: "gfx/interface/advance"},
	{Game: game.EU5, Database: "advances", Pattern: "gfx/interface/advance/%s"},
	{Game: game.EU5, Database: "advances", Pattern: "gfx/interface/advance/_default"},
	{Game: game.EU5, Database: "age", Painting: true, Pattern: "gfx/interface/illustrations/advances/%s"},
}

// findPicture returns the path of the picture a definition shows, and
// whether the game tints it. An event shows the file it names, else the one
// its chains lead to; anything else what its rows say. It is "" for none.
func findPicture(ws *workspace.Session, mods *index.Index, site index.Site) (string, bool) {
	if mods.ToReference(site).Database != ws.Schema.Events {
		return findRowPicture(ws, mods, site, false)
	}

	tree, statement, err := ws.FindStatement(mods, site)
	if err != nil {
		return "", false
	}

	return findEventPicture(ws, mods, tree, statement, lang.LookupDialect(ws.Game.ID).Events.Picture), false
}

// findPainting returns the path of the painting behind a definition's head,
// "" for none.
func findPainting(ws *workspace.Session, mods *index.Index, site index.Site) string {
	file, _ := findRowPicture(ws, mods, site, true)

	return file
}

// findRowPicture returns the file the first of a definition's rows leads to,
// and whether that row is a silhouette. It reads the painting rows where a
// painting is asked for. A later level with no file shows its line's first:
// CK3's wood_district_buildings.txt:173 writes type_icon on
// forestry_resource_building_1 alone. It returns "" if nothing leads to a
// file.
func findRowPicture(ws *workspace.Session, mods *index.Index, site index.Site, painting bool) (string, bool) {
	reference := mods.ToReference(site)
	for _, row := range pictureRows {
		if row.Game != ws.Game.ID || row.Database != reference.Database || row.Painting != painting {
			continue
		}

		if file, isFound := mods.ResolveTexture(findRowPath(ws, mods, site, row)); isFound {
			return file, row.Silhouette
		}
	}

	if head := loadModel(ws, mods).listLine(reference.ToID())[0]; head != reference.ToID() {
		if first, err := resolveNode(ws, mods, head); err == nil {
			return findRowPicture(ws, mods, first, painting)
		}
	}

	return "", false
}

// findRowPath returns the script path of the file a row names for a
// definition. It returns "" if the definition does not write it.
func findRowPath(ws *workspace.Session, mods *index.Index, site index.Site, row pictureRow) string {
	if row.Key == "" && row.Field == "" {
		return strings.ReplaceAll(row.Pattern, patternName, mods.ToReference(site).Name)
	}

	tree, statement, err := ws.FindStatement(mods, site)
	if err != nil {
		return ""
	}

	if row.Key != "" {
		return findRowValue(ws, mods, site, listValues(tree, statement, row.Key), row.Folder)
	}

	values := listValues(tree, statement, row.Field)
	if len(values) == 0 {
		return ""
	}

	return strings.ReplaceAll(row.Pattern, patternName, values[0])
}

// constantPrefix starts the name of a script constant, as @icon, and gfxFolder
// the path of a picture that names its whole folder.
const (
	constantPrefix = "@"
	gfxFolder      = "gfx/"
)

// findRowValue returns the script path of the file a key's first value
// names, through a constant and under the row's folder. It is "" if the key
// writes no value.
func findRowValue(ws *workspace.Session, mods *index.Index, site index.Site, values []string, folder string) string {
	if len(values) == 0 {
		return ""
	}

	value := values[0]
	if strings.HasPrefix(value, constantPrefix) {
		value = findConstant(ws, mods, site, value)
	}

	if value == "" || strings.HasPrefix(value, gfxFolder) {
		return value
	}

	return path.Join(folder, value)
}

// findConstant returns the text of a constant the definition's file writes,
// as @icon = "gfx/interface/icons/x.dds", "" if it writes none of that name.
func findConstant(ws *workspace.Session, mods *index.Index, site index.Site, name string) string {
	constants := mods.ResolveLocal(lang.Reference{Database: lang.Constant, Name: name}, site.File)
	if len(constants) == 0 {
		return ""
	}

	tree, statement, err := ws.FindStatement(mods, constants[len(constants)-1])
	if err != nil {
		return ""
	}

	return tree.Unquoted(tree.Statements[statement].Value)
}

// findEventPicture returns the path of the picture an event shows: the file
// it names, else the one the first of the game's chains leads to, "" for
// none.
func findEventPicture(ws *workspace.Session, mods *index.Index, tree *jomini.Tree, statement int32, rows lang.Picture) string {
	if file, isFound := resolveFile(mods, tree, statement, rows.FileKey); isFound {
		return file
	}

	for _, chain := range rows.Chains {
		if file := resolveChain(ws, mods, chain, tree, statement); file != "" {
			return file
		}
	}

	return ""
}

// resolveFile returns the image file a definition names under a key. It
// returns false if it names none, no file has that path, or the file is no
// image, as the .bk2 video of a Vic3 event.
func resolveFile(mods *index.Index, tree *jomini.Tree, statement int32, key string) (string, bool) {
	values := listValues(tree, statement, key)
	if len(values) == 0 {
		return "", false
	}

	file, isFound := mods.ResolveTexture(values[0])
	if !isFound || !texture.IsImage(file) {
		return "", false
	}

	return file, true
}

// resolveChain returns the picture a chain of definitions ends in. Where the
// last names no file, the chain's fallback key leads one step further in the
// same database. It is "" if the chain leads to no file.
func resolveChain(ws *workspace.Session, mods *index.Index, chain lang.PictureChain, tree *jomini.Tree, statement int32) string {
	for _, step := range chain.Steps {
		site, ok := findHop(ws, mods, step, listValues(tree, statement, step.Key))
		if !ok {
			return ""
		}

		var err error
		if tree, statement, err = ws.FindStatement(mods, site); err != nil {
			return ""
		}
	}

	file, isFound := resolveFile(mods, tree, statement, chain.FileKey)
	if isFound || chain.Fallback == "" {
		return file
	}

	last := lang.PictureStep{Key: chain.Fallback, Database: chain.Steps[len(chain.Steps)-1].Database}

	return resolveChain(ws, mods, lang.PictureChain{Steps: []lang.PictureStep{last}, FileKey: chain.FileKey}, tree, statement)
}

// findHop returns the definition one step of a chain leads to: the one the
// first value names, or, for a step that compares values, the first that
// writes every value. It returns false if there is none.
func findHop(ws *workspace.Session, mods *index.Index, step lang.PictureStep, values []string) (index.Site, bool) {
	if len(values) == 0 {
		return index.Site{}, false
	}

	if step.Match == "" {
		winner, ok := mods.FindKept(ws.Schema, lang.Reference{Database: step.Database, Name: values[0]})

		return winner.Site, ok
	}

	for _, candidate := range listCandidates(ws, mods, step) {
		hasEvery := !slices.ContainsFunc(values, func(value string) bool { return !slices.Contains(candidate.values, value) })
		if hasEvery {
			return candidate.site, true
		}
	}

	return index.Site{}, false
}

// candidate is one kept definition a step may lead to, with the values it
// writes under the key the step compares.
type candidate struct {
	site   index.Site
	values []string
}

// candidates holds the candidates of each comparing step for the latest
// index.
var candidates latest[lang.PictureStep, []candidate]

// listCandidates returns the candidates of a comparing step, in load order.
// A definition whose file cannot be read is left out.
func listCandidates(ws *workspace.Session, mods *index.Index, step lang.PictureStep) []candidate {
	return candidates.load(mods, step, func() []candidate {
		var list []candidate
		for _, winner := range mods.ListKept(ws.Schema, step.Database) {
			if tree, statement, err := ws.FindStatement(mods, winner.Site); err == nil {
				list = append(list, candidate{site: winner.Site, values: listValues(tree, statement, step.Match)})
			}
		}

		return list
	})
}

// listValues returns the values under keys joined by slashes. A block gives
// the values of its statements. It returns nil if there are no keys or one
// of them is not written.
func listValues(tree *jomini.Tree, statement int32, keyPath string) []string {
	if keyPath == "" {
		return nil
	}

	for key := range strings.SplitSeq(keyPath, "/") {
		child, ok := pickChild(tree, statement, key)
		if !ok {
			return nil
		}

		statement = child
	}

	if value := tree.Statements[statement].Value; !value.IsEmpty() {
		return []string{tree.Unquoted(value)}
	}

	var values []string
	for child := range tree.Children(statement) {
		if value := tree.Statements[child].Value; !value.IsEmpty() {
			values = append(values, tree.Unquoted(value))
		}
	}

	return values
}

// pickChild returns the child of a statement with a key. key[field=value]
// picks the child whose field has that value. Of several plain ones it picks
// the first with no trigger, else the first. It returns false for none.
func pickChild(tree *jomini.Tree, statement int32, key string) (int32, bool) {
	key, selector, isSelected := strings.Cut(strings.TrimSuffix(key, "]"), "[")
	field, value, _ := strings.Cut(selector, "=")
	first, ok := int32(0), false
	for child := range tree.Children(statement) {
		if tree.Text(tree.Statements[child].Key) != key {
			continue
		}

		if isSelected {
			if text, _ := tree.FindChildValue(child, field); text == value {
				return child, true
			}

			continue
		}

		if _, hasTrigger := tree.FindChild(child, lang.TriggerKey); !hasTrigger {
			return child, true
		}

		if !ok {
			first, ok = child, true
		}
	}

	return first, ok
}
