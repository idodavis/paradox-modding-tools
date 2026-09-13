// Package loc parses Paradox's localization "YAML" dialect (which is not real
// YAML). It answers "what keys/values are in this loc file?" and is
// game-agnostic. It may import jomini only for Decode.
//
// Files: parse.go, properties.go (STRICT/BROAD), lang.go. Offsets are UTF-8
// byte indices into the original text (a leading BOM is skipped, not stripped).
package loc
