export type PageId = "hub" | "compare-tool" | "merge-tool" | "inventory" | "modding-resources" | "settings";

export type HelpBlock =
  | { type: "paragraph"; text: string }
  | { type: "list"; items: string[] }
  | { type: "orderedList"; items: string[] }
  | { type: "keyValue"; items: { key: string; value: string }[] }
  | { type: "listWithSteps"; items: { main: string; steps?: string[] }[] }
  | { type: "code"; text: string };

export type HelpSection = {
  heading: string;
  blocks: HelpBlock[];
};

export type HelpConfig = {
  title: string;
  sections: HelpSection[];
};

export const helpConfig: Record<PageId, HelpConfig> = {
  hub: {
    title: "Paradox Modding Tools",
    sections: [
      {
        heading: "Overview",
        blocks: [
          {
            type: "paragraph",
            text: "Tools and utilities for Paradox mod developers. Browse script help files, compare files and directories, merge Paradox-formatted scripts, and explore extracted game objects.",
          },
        ],
      },
      {
        heading: "Getting started",
        blocks: [
          {
            type: "paragraph",
            text: "Use the cards on the Hub to open each tool. The header provides: game selector (CK3/EU5), Settings (gear icon), and Help (this dialog). Click the back arrow or Hub when inside a tool to return.",
          },
        ],
      },
    ],
  },
  "compare-tool": {
    title: "File Compare Help",
    sections: [
      {
        heading: "Overview",
        blocks: [
          {
            type: "paragraph",
            text: "Compare two file sets or single files side-by-side. Choose a mode, run the comparison, then click a row in the results to view the diff.",
          },
        ],
      },
      {
        heading: "Compare modes",
        blocks: [
          {
            type: "listWithSteps",
            items: [
              {
                main: "Vanilla vs Mod — Compare base game files with a mod. Vanilla (A) uses the game path from Settings. Select your mod folder (B) and run.",
                steps: ["Set game path in Settings if not already done", "Select mod folder (B)", "Click Run Compare"],
              },
              {
                main: "Directory vs Directory — Compare two arbitrary file sets (e.g. two mod versions). A and B are matched by path.",
                steps: ["Select directory A", "Select directory B", "Click Run Compare"],
              },
              {
                main: "File vs File — Compare two specific files directly.",
                steps: ["Select File A", "Select File B", "Click Run Compare"],
              },
            ],
          },
        ],
      },
      {
        heading: "Results",
        blocks: [
          {
            type: "paragraph",
            text: "The grid lists matched files. Click a row to view the side-by-side diff in the split pane. Use Prev/Next to move between files. Fullscreen opens a larger diff view.",
          },
        ],
      },
    ],
  },
  "merge-tool": {
    title: "Merge Tool Help",
    sections: [
      {
        heading: "Overview",
        blocks: [
          {
            type: "paragraph",
            text: "The Merge Tool combines Paradox-formatted files (e.g. CK3, EU5 scripts) by matching keys and blocks. Choose a merge mode (Vanilla vs Mod, Two Directories, or File Pairs), configure options, then Run Merge.",
          },
        ],
      },
      {
        heading: "Merge Modes",
        blocks: [
          {
            type: "listWithSteps",
            items: [
              {
                main: "Vanilla vs Mod — Use when merging vanilla/base game files with a mod.",
                steps: [
                  "Set game path in Settings (Cogwheel Icon in Header)",
                  "Select mod folder (B)",
                  "Choose output directory and Run Merge",
                ],
              },
              {
                main: "Two Directories — Use when merging two arbitrary file sets (e.g. two mod versions).",
                steps: ["Select directory A (base)", "Select directory B", "Choose output directory and Run Merge"],
              },
              {
                main: "File Pairs — Explicitly pair files when paths differ.",
                steps: ["Add pairs A↔B with an optional output name per pair", "Choose output directory and Run Merge"],
              },
            ],
          },
        ],
      },
      {
        heading: "Options",
        blocks: [
          {
            type: "keyValue",
            items: [
              {
                key: "Resolution (Auto vs Manual)",
                value:
                  "Auto: conflicts are resolved automatically (A wins by default). Manual: a merge editor opens for each file so you can choose A, B, or edit a custom result.",
              },
              {
                key: "Add entries from B",
                value:
                  "When enabled, entries present only in B are appended to the merged output. In Manual mode these appear in the Additions tab where you can include or exclude each one.",
              },
              {
                key: "Key list",
                value: "Keys where B always wins, one per line. Overrides normal conflict resolution.",
              },
              {
                key: "Match by filename only",
                value:
                  "Match files by filename instead of full path. Use when A and B have different directory structures.",
              },
              {
                key: "Include / Exclude path pattern",
                value: "Regex filters for which files to process. Examples: `.*\\.txt$`, `events/`.",
              },
              {
                key: "Output suffix",
                value: "Optional suffix added to merged filenames (e.g. `_merged`).",
              },
            ],
          },
        ],
      },
      {
        heading: "Manual Merge Editor",
        blocks: [
          {
            type: "paragraph",
            text: "Shown for each file in Manual mode. Header controls: Skip File, Save & Continue, Cancel Merge.",
          },
          {
            type: "keyValue",
            items: [
              {
                key: "Conflicts tab",
                value:
                  "Prev/Next to move between conflicts. Choose A or B for each side. Optional: edit in the Result pane for a custom resolution. Use Rest → A/B to apply one side to all remaining unresolved conflicts.",
              },
              {
                key: "Additions tab",
                value:
                  'Appears when "Add entries from B" is on. Per addition: Include/Exclude toggle, or use Include all / Exclude all. Result pane shows the text when included, "(excluded)" when not.',
              },
            ],
          },
        ],
      },
      {
        heading: "Comment Directives",
        blocks: [
          {
            type: "paragraph",
            text: "Add directives as comments above objects/entries in source files to override merge behavior. Use them to protect certain blocks in B (Mod) from being overwritten by A (Vanilla). Directives are synonymous: `# PREFER:`, `# KEEP:`, `# USE:`, `# PROTECT:`.",
          },
          {
            type: "list",
            items: ["`# PREFER:B` — Prefer B's version for this block", "`# USE:B` — Use B's version for this block"],
          },
          {
            type: "code",
            text: `# PREFER:B
some_key = {
  value = 1
}

# USE:B
other_key = {
  value = 2
}`,
          },
        ],
      },
      {
        heading: "Results Table",
        blocks: [
          {
            type: "keyValue",
            items: [
              { key: "Saved to", value: "Full output path (hover for tooltip)" },
              { key: "Delta", value: "Number of conflict blocks changed" },
              { key: "Added", value: "Number of entries added from B" },
            ],
          },
          {
            type: "paragraph",
            text: "Click a row to show the diff of the merged output against the source files.",
          },
        ],
      },
    ],
  },
  inventory: {
    title: "Inventory Explorer Help",
    sections: [
      {
        heading: "Extracting Data",
        blocks: [
          {
            type: "paragraph",
            text: "Select files or folders to scan, then choose the object types you want to extract (e.g., characters, titles, events).",
          },
          {
            type: "list",
            items: [
              "Use the file selector to add paths.",
              'Select types from the dropdown or "Select all".',
              'Click "Extract" to process. Large extractions may take time and a lot of system resources.',
            ],
          },
        ],
      },
      {
        heading: "Exploring Items",
        blocks: [
          {
            type: "paragraph",
            text: "The table shows all extracted items. Click a row to view details in the side drawer.",
          },
          {
            type: "list",
            items: [
              "Filter columns using the inputs in headers.",
              "Sort by clicking headers.",
              "View references (what objects the selected item mentions) and referrers (what objects reference the selected item) in the details drawer.",
            ],
          },
        ],
      },
    ],
  },
  "modding-resources": {
    title: "Modding Resources Help",
    sections: [
      {
        heading: "Overview",
        blocks: [
          {
            type: "paragraph",
            text: "Browse script help files (.info / readme.txt) bundled with the game, and open the official modding wiki.",
          },
        ],
      },
      {
        heading: "Script docs",
        blocks: [
          {
            type: "paragraph",
            text: "Set the game install path in the field (or in Settings). Click Scan to discover doc files in the game directory. The file tree lists all found docs; click a file to view its content in the editor pane. Use the filter to narrow by filename.",
          },
        ],
      },
      {
        heading: "Modding Wiki",
        blocks: [
          {
            type: "paragraph",
            text: "The Modding Wiki tab loads the official CK3 or EU5 modding wiki in an iframe, depending on the selected game.",
          },
        ],
      },
    ],
  },
  settings: {
    title: "Settings Help",
    sections: [
      {
        heading: "Game install directories",
        blocks: [
          {
            type: "paragraph",
            text: "Set the (top-level) install path for each game (CK3 and EU5). These are used by Modding Resources, File Compare (vanilla vs mod), and Script Merger (vanilla vs mod). Use the Browse button to select the Steam game folder.",
          },
        ],
      },
      {
        heading: "Merge output directory",
        blocks: [
          {
            type: "paragraph",
            text: "Default output directory for merge operations. When running a merge, you can override this per run.",
          },
        ],
      },
      {
        heading: "Reset data",
        blocks: [
          {
            type: "paragraph",
            text: "Permanently delete all inventories, doc cache, and patch notes. Game install paths and app constants are preserved. Use when you want a clean slate for extracted data.",
          },
        ],
      },
    ],
  },
};
