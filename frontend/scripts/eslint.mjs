// eslint.mjs runs ESLint with typescript-eslint on the JavaScript TypeScript.
//
// typescript-eslint loads `typescript` in-process, and the project's `typescript` is the native
// TypeScript 7, which has no JavaScript API (see check.mjs). So every `require("typescript")` is
// pointed at the `tsc-js` alias vue-tsc already uses.
//
// Revisit with check.mjs when typescript-eslint supports TypeScript 7.
import Module, { createRequire } from "node:module";
import path from "node:path";
import { pathToFileURL } from "node:url";

const require = createRequire(import.meta.url);
const resolve = Module._resolveFilename;
Module._resolveFilename = function (request, ...rest) {
  const alias = request === "typescript" || request.startsWith("typescript/") ? request.replace("typescript", "tsc-js") : request;
  return resolve.call(this, alias, ...rest);
};

const bin = path.join(path.dirname(require.resolve("eslint/package.json")), "bin", "eslint.js");
process.argv.splice(1, 1, bin);
await import(pathToFileURL(bin).href);
