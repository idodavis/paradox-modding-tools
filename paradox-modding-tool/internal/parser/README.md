# Parser Information

- The **Go parser is written using the participle package** for Paradox Script Language located in [`paradox_parser.go`](paradox_parser.go).
- This parser is is able to parse Paradox files into their generic parts pretty accurately and can serve as a solid base for more higherlevel scripts to sit on top of.
- It supports many building blocks of Paradox Script Syntax, including: _**Expressions, Object Blocks (including nested), Arrays/Lists, Color objects, and even Comments.**_
- I've tested it with event, landed_titles, and accolades files. I believe it should eventually be able to handle all files. If you use this and come across parsing errors please leave a issue in this repo and I can try to update the parser grammar.
- The parser is currently being used for Paradox Script merging functionality in the Paradox Modding Tool I'm building [`here`](../../README.md)

## Contributing

Pull requests and issues are welcome! Feel free to hit my up on github or open an issue and I'll try to respond as soon as I have time.

If you want to extend the parser or add new features, see the [Parser Information](#parser-information).


## License

MIT