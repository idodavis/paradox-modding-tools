/**
 * Register Paradox languages, PMT color themes, and Material+Paradox file icons.
 *
 * Language configuration supplies `#` line comments, bracket pairs, auto-close,
 * and a word pattern that keeps dotted ids as one word.
 */
import { registerExtension } from "@codingame/monaco-vscode-api/extensions";
import { ExtensionHostKind } from "@codingame/monaco-vscode-extensions-service-override";
import paradoxGrammar from "../syntaxes/paradox.tmLanguage.json";
import paradoxGuiGrammar from "../syntaxes/paradox-gui.tmLanguage.json";
import paradoxLocGrammar from "../syntaxes/paradox-loc.tmLanguage.json";
import paradoxInfoGrammar from "../syntaxes/paradox-info.tmLanguage.json";
import { registerPmtColorThemes } from "./colorThemes";
import { registerPmtFileIcons } from "./fileIcons";

/**
 * TM scopes the bracket-pair overlay must treat as comment/string.
 *
 * `as const` because the consumer types these as the union "string" | "comment"
 * | "other"; without it the values widen to string and no overload matches.
 */
const GRAMMAR_TOKEN_TYPES = { comment: "comment", string: "string" } as const;

/** Language ids that share Clausewitz/Jomini `#` line comments. */
const PARADOX_LANGUAGE_IDS = [
  "paradox",
  "paradox-gui",
  "paradox-loc",
  "paradox-info",
  "paradox-mod",
] as const;

/** Contribute Paradox grammars, language ids, color themes, and icons. */
export async function registerParadoxLanguages(): Promise<void> {
  const { registerFileUrl, whenReady, getApi } = registerExtension(
    {
      name: "pmt-paradox-languages",
      publisher: "pmt",
      version: "1.0.0",
      engines: { vscode: "*" },
      contributes: {
        languages: [
          {
            id: "paradox",
            aliases: ["Paradox Script"],
            extensions: [".txt", ".editordata", ".animsm", ".asset", ".compound"],
          },
          {
            id: "paradox-gui",
            aliases: ["Paradox GUI"],
            extensions: [".gui"],
          },
          {
            id: "paradox-loc",
            aliases: ["Paradox Localization"],
            extensions: [".yml", ".yaml"],
          },
          {
            id: "paradox-info",
            aliases: ["Paradox Info"],
            filenames: ["*.info"],
          },
          {
            id: "paradox-mod",
            aliases: ["Paradox Mod Descriptor"],
            extensions: [".mod"],
            filenames: ["descriptor.mod"],
          },
        ],
        grammars: [
          {
            language: "paradox",
            scopeName: paradoxGrammar.scopeName,
            path: "./syntaxes/paradox.tmLanguage.json",
            tokenTypes: GRAMMAR_TOKEN_TYPES,
          },
          {
            language: "paradox-gui",
            scopeName: paradoxGuiGrammar.scopeName,
            path: "./syntaxes/paradox-gui.tmLanguage.json",
            tokenTypes: GRAMMAR_TOKEN_TYPES,
          },
          {
            language: "paradox-loc",
            scopeName: paradoxLocGrammar.scopeName,
            path: "./syntaxes/paradox-loc.tmLanguage.json",
            tokenTypes: GRAMMAR_TOKEN_TYPES,
          },
          {
            language: "paradox-info",
            scopeName: paradoxInfoGrammar.scopeName,
            path: "./syntaxes/paradox-info.tmLanguage.json",
            tokenTypes: GRAMMAR_TOKEN_TYPES,
          },
          {
            language: "paradox-mod",
            scopeName: paradoxGrammar.scopeName,
            path: "./syntaxes/paradox.tmLanguage.json",
            tokenTypes: GRAMMAR_TOKEN_TYPES,
          },
        ],
      },
    },
    ExtensionHostKind.LocalProcess,
  );

  registerFileUrl(
    "./syntaxes/paradox.tmLanguage.json",
    `data:application/json,${encodeURIComponent(JSON.stringify(paradoxGrammar))}`,
  );
  registerFileUrl(
    "./syntaxes/paradox-gui.tmLanguage.json",
    `data:application/json,${encodeURIComponent(JSON.stringify(paradoxGuiGrammar))}`,
  );
  registerFileUrl(
    "./syntaxes/paradox-loc.tmLanguage.json",
    `data:application/json,${encodeURIComponent(JSON.stringify(paradoxLocGrammar))}`,
  );
  registerFileUrl(
    "./syntaxes/paradox-info.tmLanguage.json",
    `data:application/json,${encodeURIComponent(JSON.stringify(paradoxInfoGrammar))}`,
  );

  await whenReady();
  const vscode = await getApi();
  const pairs = [
    { open: "{", close: "}" },
    { open: "[", close: "]" },
    { open: "(", close: ")" },
    { open: '"', close: '"' },
  ];
  const brackets: [string, string][] = [
    ["{", "}"],
    ["[", "]"],
    ["(", ")"],
  ];
  const scriptWord = /[A-Za-z0-9_.\-]+/;
  const locWord = /[A-Za-z0-9_$.\-]+/;
  for (const id of PARADOX_LANGUAGE_IDS) {
    // monaco-vscode LanguageConfiguration omits surroundingPairs; VS Code honors it.
    vscode.languages.setLanguageConfiguration(id, {
      comments: { lineComment: "#" },
      brackets,
      autoClosingPairs: pairs,
      surroundingPairs: pairs,
      colorizedBracketPairs: [
        ["{", "}"],
        ["[", "]"],
      ],
      wordPattern: id === "paradox-loc" ? locWord : scriptWord,
    } as Parameters<typeof vscode.languages.setLanguageConfiguration>[1]);
  }
  await registerPmtColorThemes();
  await registerPmtFileIcons();
}
