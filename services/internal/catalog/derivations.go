// derivations.go declares how far each field of Derived travels (plan II.3).

package catalog

// DeriveScope says how far a projection travels.
type DeriveScope string

const (
	// ScopeInstall: derived on the install walk only; a mod walk reads the cache.
	ScopeInstall DeriveScope = "install"
	// ScopeInstallMod: a mod's contribution reaches the session via Harvest.
	ScopeInstallMod DeriveScope = "install+mod"
	// ScopeModLocal: a mod walk derives it and applyDerivedDefs uses it, but
	// Harvest has no field for it, so it never leaves collectExtracts.
	ScopeModLocal DeriveScope = "mod-local"
)

// Derivation is one projection of Derived. Reason is empty where the codebase
// does not record one; five do not.
type Derivation struct {
	Name   string
	Scope  DeriveScope
	Reason string
}

var Derivations = []Derivation{
	{"FieldValueKinds", ScopeInstallMod,
		"Session keeps modFieldKinds; MergeFieldValueKinds drops fields the two disagree on."},
	{"FireKeys", ScopeInstallMod, "Session keeps modFireKeys; the Event Graph needs it."},

	{"PrefixKinds", ScopeModLocal, ""},
	{"KindScope", ScopeModLocal, ""},
	{"NestedShapes", ScopeModLocal, ""},
	{"Wrappers", ScopeModLocal, ""},

	{"LocAffixes", ScopeInstall, ""},
	{"LocFields", ScopeInstall,
		"collectExtracts gets no loc key set on the mod path; see the call site."},
	{"LocListFields", ScopeInstall, "As LocFields."},
}
