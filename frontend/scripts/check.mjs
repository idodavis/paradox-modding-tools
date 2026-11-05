// check.mjs type-checks the app, .vue files included.
//
// vue-tsc cannot use the project's own TypeScript. TypeScript 7 is a native
// binary: its lib/tsc.js is a 609-byte launcher that execve's an executable, and
// lib/typescript.js does not exist at all. vue-tsc works by loading the
// JavaScript tsc in-process and patching it to understand SFCs, which is not
// something a native binary can be asked to do.
//
// So the checker gets its own JavaScript TypeScript, installed under the alias
// `tsc-js`. The project's `typescript` stays on 7 and is what tsc, the editor
// and the build use; this is only the engine vue-tsc drives. vue-tsc's own
// resolveTscPath does the same thing for the `@typescript/typescript6` alias,
// so passing an explicit path is the supported route rather than a workaround.
//
// Revisit when vue-tsc supports TypeScript 7 natively; then this file and the
// tsc-js dependency both go away.
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
require("vue-tsc").run(require.resolve("tsc-js/lib/tsc"));
