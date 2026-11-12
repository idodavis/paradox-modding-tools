// fires.go lists which definitions fire which: an event, an on_action or a
// decision that triggers another, directly or through the scripted effects
// between them.

package index

import (
	"slices"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/schema"
)

// Fire is one firing: one definition fires another, through the reference
// that names it and the scripted effects the firing passes through.
type Fire struct {
	Source Site `json:"source"`
	Target Site `json:"target"`

	// Use is the reference that names the target, beside which a delay is
	// written.
	Use Site `json:"use"`

	// Via holds the scripted effects entered between the two, in order.
	Via []Site `json:"via,omitempty"`
}

// fireWalk is one walk along the firings from one definition, through the
// scripted effects between if asked. The scripted effects on the way are
// open, so that effects that call each other end the walk.
type fireWalk struct {
	index  *Index
	schema *schema.Schema

	// start is the definition the walk starts from, and follow whether it
	// goes through scripted effects.
	start  Site
	follow bool

	// open holds the definitions on the way from the start.
	open map[Site]bool

	fires []Fire
}

// ListFires returns the events, on_actions and decisions a definition fires,
// through its scripted effects if asked. It returns nil if it fires nothing.
func (i *Index) ListFires(schema *schema.Schema, site Site, follow bool) []Fire {
	walk := &fireWalk{index: i, schema: schema, start: site, follow: follow, open: map[Site]bool{}}
	walk.forward(site, nil)

	return walk.fires
}

// ListFiredBy returns the definitions that fire a definition, through
// scripted effects if asked. It returns nil if nothing fires it.
func (i *Index) ListFiredBy(schema *schema.Schema, site Site, follow bool) []Fire {
	if !slices.Contains(schema.FireDatabases, i.ToReference(site).Database) {
		return nil
	}

	walk := &fireWalk{index: i, schema: schema, start: site, follow: follow, open: map[Site]bool{}}
	walk.backward(site, nil, Site{})

	return walk.fires
}

// forward adds what a definition fires, and goes on into each scripted effect
// it calls where the walk follows them. The given effects are those entered
// on the way.
func (f *fireWalk) forward(current Site, effects []Site) {
	f.open[current] = true
	defer delete(f.open, current)

	for _, use := range f.index.listReferences(current) {
		if !f.index.Occurrence(use).Firing {
			continue
		}

		for _, reference := range ListCallees(f.index.ToReference(use)) {
			targeted := slices.Contains(f.schema.FireDatabases, reference.Database)
			if !targeted && (!f.follow || reference.Database != f.schema.Effects) {
				continue
			}

			for _, target := range f.index.ListActing(f.schema, reference, use.File) {
				switch {
				case targeted:
					f.fires = append(f.fires, Fire{Source: f.start, Target: target, Use: use, Via: effects})
				case !f.open[target]:
					f.forward(target, append(slices.Clone(effects), target))
				}
			}
		}
	}
}

// backward adds what fires a definition, and goes on back from each scripted
// effect that fires it where the walk follows them. The given effects are
// those passed on the way, and the given use is the reference that names the
// start, found at the first step.
func (f *fireWalk) backward(current Site, effects []Site, use Site) {
	f.open[current] = true
	defer delete(f.open, current)

	// Only a definition that acts is called by name.
	reference := f.index.ToReference(current)
	if winner, ok := f.index.FindKeptLocal(f.schema, reference, current.File); !ok || !f.index.isActing(winner, current) {
		return
	}

	for _, referrer := range f.index.ListUses(reference.Database, reference.Name) {
		parent, nested := f.index.FindDefinition(referrer)
		if !nested || !f.index.Occurrence(referrer).Firing {
			continue
		}

		startUse := use
		if current == f.start {
			startUse = referrer
		}

		holder := f.index.ToReference(parent)
		switch {
		case holder.Database != f.schema.Effects || !f.follow:
			// A firer the game drops fires nothing, and a merged one stands as
			// the definition the game keeps.
			winner, ok := f.index.FindKeptLocal(f.schema, holder, parent.File)
			if !ok || !f.index.isActing(winner, parent) {
				continue
			}

			source := winner.Site
			if winner.Reason == game.ReasonUnsettled {
				source = parent
			}

			f.fires = append(f.fires, Fire{Source: source, Target: f.start, Use: startUse, Via: effects})

		case !f.open[parent]:
			f.backward(parent, slices.Insert(slices.Clone(effects), 0, parent), startUse)
		}
	}
}

// listReferences returns the references that sit directly inside a
// definition, in source order.
func (i *Index) listReferences(definition Site) []Site {
	var sites []Site

	// A definition's references follow it without a break, so the list ends
	// at the first occurrence that is not inside it.
	occurrences := i.Occurrences[definition.File]
	for position := definition.Occurrence + 1; int(position) < len(occurrences); position++ {
		occurrence := occurrences[position]
		if !occurrence.Nested || occurrence.Parent < definition.Occurrence {
			break
		}

		if occurrence.Role == RoleRefers && occurrence.Parent == definition.Occurrence {
			sites = append(sites, Site{File: definition.File, Occurrence: position})
		}
	}

	return sites
}

// ListActing returns the copies of a name that a reference in a file reaches.
// It returns none if the game keeps none.
func (i *Index) ListActing(schema *schema.Schema, reference schema.Reference, file int32) []Site {
	winner, ok := i.FindKeptLocal(schema, reference, file)
	if !ok {
		return nil
	}

	return slices.DeleteFunc(i.ResolveLocal(reference, file), func(site Site) bool { return !i.isActing(winner, site) })
}

// isActing reports whether a definition of the name a winner won takes part
// in what the game runs: the one it keeps, one merged into it, or, where no
// source says which it keeps, any copy in a file it reads.
func (i *Index) isActing(winner Winner, site Site) bool {
	outcome := i.ResolveSite(winner, site)

	return outcome.Kept || outcome.Reason == game.ReasonUnsettled
}

// firingFields holds, by database, the key paths in its definitions where a
// name fires the event, on_action or decision it names, as it would in
// effect script. A row holds for the games its Games lists.
var firingFields = map[string][]game.Reference{
	// CK3 common/council_tasks/00_chancellor_tasks.txt:150
	"council_tasks": {{Games: []string{game.CK3}, KeyPath: "monthly_on_action"}},
	// CK3 common/schemes/scheme_types/coerce_contribution_scheme.txt:143
	"schemes/scheme_types": {{Games: []string{game.CK3}, KeyPath: "on_monthly/**"}},
	// CK3 common/defines/00_defines.txt:1813-1816: "Default event to run when
	// a player province is infected"
	"defines.define": {
		{Games: []string{game.CK3}, KeyPath: "DEFAULT_HOLDER_INFECTION_EVENT"},
		{Games: []string{game.CK3}, KeyPath: "DEFAULT_LIEGE_INFECTION_EVENT"},
		{Games: []string{game.CK3}, KeyPath: "DEFAULT_REALM_INFECTION_EVENT"},
	},
	// CK3 common/puppets/actions/puppet_actions.txt:26
	"puppets/actions": {{Games: []string{game.CK3}, KeyPath: "decision"}},
	// Vic3 journal_entries.md:354-375: the fields that fire the events they
	// name, as journal_entries.md:357, 00_belle_epoque.txt:215,
	// 00_acw_entries.txt:61,120,68 and journal_entries.md:373-375 write them
	"journal_entries": {
		{Games: []string{game.Vic3}, KeyPath: "on_weekly_pulse/random_events/<number>"},
		{Games: []string{game.Vic3}, KeyPath: "on_weekly_pulse/events"},
		{Games: []string{game.Vic3}, KeyPath: "on_monthly_pulse/random_events/<number>"},
		{Games: []string{game.Vic3}, KeyPath: "on_monthly_pulse/events"},
		{Games: []string{game.Vic3}, KeyPath: "on_yearly_pulse/random_events/<number>"},
		{Games: []string{game.Vic3}, KeyPath: "on_yearly_pulse/events"},
	},
}

// isFiring reports whether a reference fires the event, on_action or decision
// it names.
func (c *collector) isFiring(context *schema.Context, reference schema.Reference) bool {
	if context.Conditional || context.Preview || context.Category == schema.CategoryLink {
		return false
	}

	row := c.index.row
	effect := context
	if !isEffect(effect, c.schema.Effects) {
		effect = context.Parent
	}

	if effect != nil && isEffect(effect, c.schema.Effects) {
		target := c.schema.Databases[reference.Database]

		return target == nil || target.Declaration.Contents != game.ContentsDecisions ||
			slices.Contains(row.DecisionEffects, effect.Label)
	}

	if holder := c.schema.Databases[context.Database]; holder != nil && holder.Declaration.Contents == game.ContentsOnActions {
		return true
	}

	keys := context.Path
	if context.Label != "" {
		keys = slices.Concat(keys, []string{context.Label})
	}

	return slices.ContainsFunc(firingFields[context.Database], func(field game.Reference) bool {
		return row.Matches(field.Games) && jomini.MatchKeyPath(field.KeyPath, keys)
	})
}

// isEffect reports whether a statement is an effect or a call of a scripted
// effect.
func isEffect(context *schema.Context, effects string) bool {
	return context.Command != nil && context.Command.Database == game.DumpEffects ||
		slices.ContainsFunc(context.References, func(reference schema.Reference) bool {
			return reference.Database == effects || reference.Database == game.AnyCall
		})
}
