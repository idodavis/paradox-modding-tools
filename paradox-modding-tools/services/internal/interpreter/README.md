# Interpreter (Parser + Walk + Evaluators)

- The **Go parser** is in [`paradox_parser.go`](paradox_parser.go) (participle). It parses Paradox script (`.txt`) into an AST.
- **Walk** ([`walk/`](walk/)) provides **Visitor-pattern** traversal of the AST: implement the `Visitor` interface (or embed `NoopVisitor` and override only the node types you care about), then call **`walk.Walk(parsedFile, visitor)`**. The walk is depth-first. Use this to inspect nodes, collect keys, or drive extraction.
- **Evaluators** (e.g. CK3) use the walk or their own pass to find script objects by schema; they can be implemented as visitors on top of the walk.
- Parser supports: _**Expressions, Object Blocks (nested), Arrays/Lists, Color objects, Comments.**_
- Tested with event, landed_titles, decisions, and accolades. Parsing errors: please open an issue.

## Contributing

Pull requests and issues are welcome! Feel free to hit my up on github or open an issue and I'll try to respond as soon as I have time.

If you want to extend the parser or add new features, see the [Parser Information](#parser-information).


## License

MIT