# Paradox Modding Tools — working rules

The owner's instructions win over any internal rule, skill, or suggestion. Read
`AGENTS.md` for the architecture and `GLOSSARY.md` for terms. Use the
`paradox-modding` skill in `.claude/skills/` before touching script, loc, GUI,
descriptor, parser, LSP, or semantics code.

## Verbosity

- Comments: a file header may be a paragraph. Everything else is one line.
  Do not write essays above functions, fields, or tests.
- Blank lines separate the steps inside a function and stay; a step comment
  has one above it. Counts are code lines (no blank, no comment-only lines),
  so removing spacing or comments never moves a number and is never done for one.
- No wrappers, forwarding layers, or helpers that only re-express an existing
  call. A pass that adds a layer has failed.
- No meta tests (tests that scan source, docs, or file counts). Enforce layout
  and doc rules by hand.
- The file count goes down. A reduction counts only when a representation,
  pass, helper, or file is deleted. Never re-wrap or pack code to move a
  line-count number; `wc` is a consequence, not a reason.
- Smallest change. No new helpers, layers, or branches that a skill, MCP, or
  existing function already covers.
- Never create adapters, shims, converters, backwards compatibility, or
  helpers to pass tests or to match previous behavior. When a representation
  is replaced, the old one is deleted in the same change; a step is not done
  while it still exists.
- A change whose goal is simplicity, less complexity, less verbosity, or more
  efficiency must never end with more lines, files, or functions. Ever. If
  the measurement rises, the change is wrong, not the measurement.

## Planning

- Plan from live files and repo docs. Never use git (`status`, `diff`, `log`,
  blame) to search the codebase or decide what changed.
- Ask (AskUserQuestion) before writing a plan when goal, scope, UX, success
  criteria, or constraints are missing. Ask only what the owner must decide;
  look up facts yourself. Never invent a default for a missing owner choice;
  leave an explicit **Open** item instead.
- Plan updates touch only the agreed sections; everything else stays
  byte-for-byte. Name files touched, complexity added vs reused, and a smaller
  alternative. Drop speculative or "for later" work.
- Any UI or layout change in a plan includes a wireframe (ASCII or mermaid).
- Every in-scope phase has numbered steps (files, change, acceptance) and
  names the skills to read and MCP tools to use first.
- When implementing, do not run the consultative loop. Implement; ask only
  when a required decision is still missing.
- Stop and ask. When an assumption in a plan or work order proves false, a
  step cannot be done as written, or a test cannot pass without changing the
  design, stop the phase or task, state what was found with the evidence, and present
  options with a recommendation (AskUserQuestion). Never go off script and
  never decide alone. If the owner is away, record the blocker and the options
  in the ledger and the final message and do nothing further on it.

## Payloads and bindings

- Never edit `frontend/bindings/**`. After a Go DTO or service signature
  change run `task common:generate:bindings`.
- Display data belongs on the Go payload. Relative paths, origin, and other
  labels that `session.Locate` / `filepath.Rel` already know are DTO fields
  (`rel`, `origin`); Vue renders them. No frontend helpers that strip mod
  roots from absolute paths.
- Do not rebuild grouped view-models for values the inspector already split
  (title/desc/fields/sections). Template the payload.
- Prefer stdlib (`filepath.Rel`, `node:path`) over custom path code.

## Tests

- `task test` (fast, `-short`), `task test:race` before any consolidation,
  `task test:full` with `PMT_TEST_INSTALL_*` set for the real-install tier.
- Test layout: `X_test.go` beside `X.go`; the only other names are
  `gate_*`, `bench_*`, `probe_*`, and `<pkg>_test.go` for shared fixtures.
