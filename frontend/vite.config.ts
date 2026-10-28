/** vite.config.ts builds the frontend and sets Nuxt UI's look to the app's theme. */
import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";
import ui from "@nuxt/ui/vite";
import wails from "@wailsio/runtime/plugins/vite";
import tailwindcss from "@tailwindcss/vite";
import path from "path";

// field sizes the app's inputs: 28px small, 32px default
const field = {
  sizes: {
    sm: {
      base: "h-7 px-2 py-0 text-12 gap-2",
      leading: "ps-2",
      trailing: "pe-2",
      leadingIcon: "size-3.5",
      trailingIcon: "size-3.5",
    },
    md: {
      base: "h-8 px-2.5 py-0 text-13 gap-2",
      leading: "ps-2.5",
      trailing: "pe-2.5",
      leadingIcon: "size-3.5",
      trailingIcon: "size-3.5",
    },
  },
  variant: { outline: "bg-(--bg-0) ring-accented" },
  compoundVariants: [
    { color: "primary", variant: "outline", class: "outline-primary/15 focus-visible:outline-2" },
    { leading: true, size: "sm", class: "ps-7.5" },
    { leading: true, size: "md", class: "ps-8" },
    { trailing: true, size: "sm", class: "pe-7.5" },
    { trailing: true, size: "md", class: "pe-8" },
    // Nuxt UI changes the text size above md; the field keeps its own
    { fixed: false, size: "sm", class: "md:text-12" },
    { fixed: false, size: "md", class: "md:text-13" },
  ],
};
const menu = {
  slots: {
    content: "bg-elevated rounded-lg ring-0 shadow-(--shadow)",
    group: "p-1",
    separator: "mx-1.5 my-1",
    item: "items-center before:inset-0 before:rounded-sm data-highlighted:not-data-disabled:before:bg-(--accent-soft)",
  },
  item: {
    label: "px-2.5 pt-2 pb-1 text-11 font-medium text-dimmed",
    item: "h-7 px-2.5 py-0 text-13 gap-2.5",
    itemLeadingIcon: "size-3.5 text-dimmed",
    itemTrailingIcon: "size-3.5",
  },
};
// chatRow sizes a tool call's and a thought's row in an agent's transcript: 12px text and 14px icons
const chatRow = {
  slots: {
    trigger: "text-12",
    leading: "size-3.5",
    leadingIcon: "size-3.5",
    chevronIcon: "size-3.5",
    trailingIcon: "size-3.5",
    body: "text-12",
  },
};
// step draws a stepper's circle: green when done, the accent when current
const step = {
  trigger: [
    "size-5.5 text-11 font-medium ring ring-inset ring-accented bg-transparent text-dimmed",
    "group-data-[state=completed]:bg-(--green-soft) group-data-[state=completed]:text-success group-data-[state=completed]:ring-0",
    "group-data-[state=active]:bg-primary group-data-[state=active]:text-(--accent-ink) group-data-[state=active]:ring-primary",
  ],
  icon: "size-3.5",
  wrapper: "mt-0",
};
const listbox = {
  slots: menu.slots,
  variants: {
    size: { sm: { ...field.sizes.sm, ...menu.item }, md: { ...field.sizes.md, ...menu.item } },
    variant: field.variant,
  },
  compoundVariants: field.compoundVariants,
};
// contextual styles the dropdown and context menus
const contextual = {
  slots: menu.slots,
  variants: {
    size: { md: menu.item },
    active: {
      false: { item: "data-highlighted:before:bg-(--accent-soft) data-[state=open]:before:bg-(--accent-soft)" },
    },
  },
};

export default defineConfig({
  plugins: [
    vue(),
    ui({
      colorMode: true,
      // only Nuxt UI's components are resolved by name
      components: { dirs: [] },
      theme: {
        colors: ["primary", "secondary", "success", "info", "warning", "error", "purple"],
      },
      ui: {
        // a pane's resize handle is a 2px line, grabbed on a wider strip above the panes' own layers
        splitter: {
          slots: {
            handle:
              "z-20 before:absolute after:absolute after:bg-(--line-1) data-[state=hover]:after:bg-primary data-[state=drag]:after:bg-primary",
          },
          variants: {
            orientation: {
              horizontal: {
                handle:
                  "w-0 before:inset-y-0 before:-inset-x-1 after:inset-y-0 after:start-1/2 after:w-0.5 after:-translate-x-1/2",
              },
              vertical: {
                handle:
                  "h-0 before:inset-x-0 before:-inset-y-1 after:inset-x-0 after:top-1/2 after:h-0.5 after:-translate-y-1/2",
              },
            },
          },
        },
        // themes.css's text sizes and radii, so merged classes keep them
        tv: {
          twMergeConfig: {
            extend: {
              theme: {
                text: ["11", "11.5", "12", "13", "14", "16", "18", "20", "28", "tiny"],
                radius: ["chip", "tile"],
              },
            },
          },
        },
        button: {
          slots: { leadingIcon: "size-3.5", trailingIcon: "size-3.5" },
          variants: {
            size: {
              xs: { base: "h-6 px-2 py-0 text-12 gap-1.5", leadingIcon: "size-3.5", trailingIcon: "size-3.5" },
              sm: { base: "h-7 px-2.5 py-0 text-12 gap-1.5", leadingIcon: "size-3.5", trailingIcon: "size-3.5" },
              md: { base: "h-8 px-3.5 py-0 text-13 gap-1.5", leadingIcon: "size-3.5", trailingIcon: "size-3.5" },
            },
          },
          compoundVariants: [
            { color: "neutral", variant: "outline", class: "bg-elevated text-highlighted hover:bg-accented" },
            { color: "neutral", variant: "ghost", class: "text-toned hover:bg-accented hover:text-highlighted" },
            // pressed
            { color: "neutral", variant: "soft", class: "bg-accented text-highlighted ring ring-inset ring-accented" },
            { color: "primary", variant: "solid", class: "text-(--accent-ink) hover:bg-primary/85" },
            // on
            {
              color: "primary",
              variant: "soft",
              class: {
                base: "bg-(--accent-soft) text-highlighted ring ring-inset ring-primary/45",
                leadingIcon: "text-primary",
              },
            },
            // danger
            { color: "error", variant: "soft", class: "bg-(--red-soft) hover:bg-(--red-soft)" },
            { square: true, size: "xs", class: { base: "size-5.5 px-0 justify-center", leadingIcon: "size-3.5" } },
            { square: true, size: "sm", class: { base: "w-7 px-0 justify-center", leadingIcon: "size-4" } },
            { square: true, size: "md", class: { base: "w-8 px-0 justify-center", leadingIcon: "size-4" } },
          ],
          defaultVariants: { color: "neutral", variant: "outline" },
        },
        input: { variants: { size: field.sizes, variant: field.variant }, compoundVariants: field.compoundVariants },
        select: listbox,
        selectMenu: listbox,
        inputMenu: listbox,
        dropdownMenu: contextual,
        contextMenu: contextual,
        // content stays inside the rounded corners
        popover: { slots: { content: "overflow-hidden bg-elevated rounded-xl ring-0 shadow-(--shadow)" } },
        // a toast has a menu's shadow in place of the ring
        toast: { slots: { root: "ring-0 shadow-(--shadow)" } },
        // a dialog has 12px corners and the theme's shadow
        modal: {
          slots: { content: "bg-elevated" },
          variants: { fullscreen: { false: { content: "rounded-xl ring-0 shadow-(--shadow)" } } },
        },
        switch: {
          slots: {
            base: "border border-accented p-0.5 data-[state=checked]:border-primary",
            thumb: "bg-(--fg-3) shadow-none data-[state=checked]:bg-(--accent-ink)",
          },
          variants: {
            size: {
              md: {
                base: "w-7.5",
                container: "h-4.5",
                thumb: "size-3 data-[state=checked]:translate-x-3 data-[state=checked]:rtl:-translate-x-3",
              },
            },
          },
        },
        checkbox: {
          slots: { base: "bg-(--bg-0)", indicator: "text-(--accent-ink)" },
          variants: { size: { md: { base: "size-4", icon: "size-3", container: "h-4.5" } } },
        },
        // the command palette: a 46px search and 32px rows
        commandPalette: {
          slots: {
            label: "font-medium text-dimmed",
            item: "items-center",
            itemLeadingIcon: "text-dimmed",
            itemLabelPrefix: "text-dimmed",
            footer: "px-3.5 py-2",
          },
          variants: {
            size: {
              md: {
                input: "[&>input]:h-11.25 [&>input]:text-14",
                label: "px-2.5 pt-2 pb-1 text-11",
                item: "h-8 px-3 py-0 text-13 gap-2.5",
                itemLeadingIcon: "size-3.5",
                itemTrailing: "gap-2",
              },
            },
          },
          compoundVariants: [
            { active: false, class: { item: "data-highlighted:not-data-disabled:before:bg-(--accent-soft)" } },
          ],
        },
        // a field's label sits over its control; horizontal is a settings row with the control at the end
        formField: {
          slots: {
            label: "text-12 font-medium text-toned",
            description: "text-11 text-dimmed",
            hint: "text-11 text-dimmed",
            help: "mt-1.25 text-11 text-dimmed",
            error: "mt-1.25 text-11",
          },
          variants: {
            size: { md: { root: "text-12" } },
            orientation: {
              vertical: { container: "mt-1.25" },
              horizontal: {
                root: "min-h-11 items-center gap-4 px-3.5 py-1.75",
                label: "text-13 leading-4.5 font-medium text-highlighted",
                description: "text-11 leading-3.75 text-dimmed",
              },
            },
          },
        },
        // a detail column's sections, each under a 32px head
        accordion: {
          slots: {
            trigger: "h-8 ps-3 pe-2 py-0 gap-1.5 text-12 font-semibold text-toned rounded-none",
            trailingIcon: "size-3.5 text-dimmed",
            body: "pb-1 text-12",
          },
        },
        // 22px rows; the picked one has the accent's fill and a 2px bar, and a deep row scrolls the panel
        tree: {
          slots: {
            itemWithChildren: "ps-1 -ms-px",
            linkLabel: "font-mono text-11.5 text-clip whitespace-nowrap",
          },
          variants: {
            size: {
              md: {
                listWithChildren: "ms-2",
                link: "h-5.5 px-2 py-0 gap-1.5 text-12 rounded-sm before:rounded-sm",
                linkLeadingIcon: "size-3.5",
                linkTrailingIcon: "size-3.5",
              },
            },
          },
          compoundVariants: [
            {
              color: "primary",
              class: {
                link: "data-selected:bg-(--accent-soft) data-selected:text-highlighted data-selected:shadow-[inset_2px_0_0_var(--accent)] data-selected:before:bg-transparent",
              },
            },
          ],
        },
        // a description wraps where Steam's does, in a 639px column at 14/20
        editor: {
          slots: {
            base: [
              "mx-auto w-159.75 max-w-full px-0 py-4 sm:px-0 *:my-2.5 text-14 text-toned [&_p]:leading-5 [&_li]:leading-5 [&_strong]:font-semibold",
              "[&_:is(h1,h2,h3,h4,h5,h6)]:font-semibold [&_h1]:text-20 [&_h2]:text-18 [&_h3]:text-16 [&_:is(h4,h5,h6)]:text-13",
              "[&_table]:w-full [&_table]:border-collapse [&_:is(th,td)]:border [&_:is(th,td)]:border-default [&_:is(th,td)]:px-2.5 [&_:is(th,td)]:py-1.5 [&_:is(th,td)]:align-top [&_th]:bg-elevated [&_th]:text-start [&_th]:font-semibold [&_th]:text-highlighted [&_.selectedCell]:bg-primary/15",
            ],
          },
        },
        // an agent's prompt box types at the fields' 13px
        chatPrompt: { slots: { base: "text-13" } },
        chatTool: chatRow,
        chatReasoning: chatRow,
        // horizontal in the Wizard, vertical in the Publish rail
        stepper: {
          compoundVariants: [
            {
              orientation: "horizontal",
              class: {
                ...step,
                root: "gap-6",
                header: "items-center gap-3",
                item: "flex w-auto flex-1 items-center gap-2 text-start last:flex-none",
                container: "contents",
                separator: "static order-last h-px flex-1 bg-accented group-data-[state=completed]:bg-accented",
                title:
                  "text-12 font-medium whitespace-nowrap text-dimmed group-data-[state=completed]:text-toned group-data-[state=active]:text-highlighted",
              },
            },
            {
              orientation: "vertical",
              class: {
                ...step,
                header: "w-full gap-1.5",
                // each step is a card that is wholly its button
                item: [
                  "items-center gap-2.5 rounded-md px-2.5 py-2.25 ring ring-inset ring-default hover:bg-elevated/60",
                  "data-[state=active]:bg-elevated data-[state=active]:ring-accented",
                  "data-disabled:opacity-60 data-disabled:hover:bg-transparent",
                ],
                container: "static",
                trigger: [
                  ...step.trigger,
                  "cursor-pointer after:absolute after:inset-0 after:rounded-md group-data-disabled:cursor-not-allowed",
                ],
                separator: "hidden",
                title:
                  "text-13 font-medium text-dimmed group-data-[state=completed]:text-toned group-data-[state=active]:text-highlighted",
                description: "text-11 text-dimmed",
              },
            },
          ],
        },
        // a card option takes the accent's fill when picked
        radioGroup: {
          compoundVariants: [
            { variant: "card", class: { item: "rounded-md bg-muted px-3 py-2.5 gap-2.5" } },
            {
              color: "primary",
              variant: "card",
              class: { item: "has-data-[state=checked]:border-primary has-data-[state=checked]:bg-(--accent-soft)" },
            },
          ],
        },
        tabs: {
          slots: { trigger: "font-medium", leadingIcon: "size-3.5" },
          variants: {
            size: { md: { trigger: "px-2.5 py-0 text-12 gap-1.5", leadingIcon: "size-3.5" } },
            // pill is a segmented control, link a row of page tabs
            variant: {
              pill: {
                list: "h-7 p-0.5 gap-0.5 rounded-md bg-muted ring ring-inset ring-default",
                trigger: "grow-0 h-5.5 rounded-sm text-toned",
                indicator: "rounded-sm bg-accented shadow-none",
              },
              link: { list: "py-0 ps-2 pe-1.5 gap-0.5", trigger: "h-8 text-dimmed", indicator: "h-0.5 -bottom-px" },
            },
          },
          compoundVariants: [
            {
              variant: "pill",
              class: { indicator: "inset-y-0.5 bg-accented", trigger: "data-[state=active]:text-highlighted" },
            },
            // a pill list is as wide as its buttons
            { orientation: "horizontal", variant: "pill", class: { root: "items-start", list: "w-fit" } },
            { variant: "link", class: { indicator: "bg-primary", trigger: "data-[state=active]:text-highlighted" } },
          ],
          defaultVariants: { color: "neutral" },
        },
        badge: {
          slots: { base: "rounded-chip font-medium" },
          variants: {
            size: {
              sm: { base: "h-4 px-1.25 py-0 text-tiny gap-1 rounded-chip" },
              md: { base: "h-5 px-1.75 py-0 text-11 gap-1 rounded-chip" },
            },
          },
          compoundVariants: [
            { color: "neutral", variant: "soft", class: "bg-accented text-toned" },
            // each color's soft fill, written out so Tailwind finds the class
            { color: "primary", variant: "soft", class: "bg-(--accent-soft) text-primary" },
            { color: "secondary", variant: "soft", class: "bg-(--link-soft) text-secondary" },
            { color: "success", variant: "soft", class: "bg-(--green-soft) text-success" },
            { color: "info", variant: "soft", class: "bg-(--blue-soft) text-info" },
            { color: "warning", variant: "soft", class: "bg-(--orange-soft) text-warning" },
            { color: "error", variant: "soft", class: "bg-(--red-soft) text-error" },
            { color: "purple", variant: "soft", class: "bg-(--purple-soft) text-(--purple)" },
          ],
          defaultVariants: { color: "neutral", variant: "soft" },
        },
        kbd: {
          base: "normal-case font-mono rounded-sm",
          variants: { size: { md: "h-4.5 min-w-4.5 px-1.25 text-tiny" } },
          compoundVariants: [{ color: "neutral", variant: "outline", class: "bg-muted text-dimmed ring-accented" }],
        },
        table: {
          slots: {
            base: "w-full table-fixed",
            th: "h-7.5 px-2.5 py-0 text-12 font-medium text-dimmed bg-muted",
            td: "h-7.5 px-2.5 py-0 text-12 text-toned",
            tr: "data-[selected=true]:bg-(--accent-soft) data-[selected=true]:text-highlighted",
          },
        },
      },
    }),
    wails("./bindings"),
    tailwindcss(),
  ],
  server: {
    host: "127.0.0.1",
    port: Number(process.env.WAILS_VITE_PORT) || 9245,
    strictPort: true,
  },
  build: { target: "esnext" },
  optimizeDeps: {
    include: [
      "@nuxt/ui > prosemirror-state",
      "@nuxt/ui > prosemirror-transform",
      "@nuxt/ui > prosemirror-model",
      "@nuxt/ui > prosemirror-view",
      "@nuxt/ui > prosemirror-gapcursor",
    ],
    // Nuxt UI loads Tiptap unbundled; the table extension must share that copy
    exclude: ["@tiptap/extension-table"],
  },
  resolve: {
    alias: {
      "@services": path.resolve(import.meta.dirname, "bindings/paradox-modding-tools/services"),
      "@internal": path.resolve(import.meta.dirname, "bindings/paradox-modding-tools/internal"),
      "@assets": path.resolve(import.meta.dirname, "src/assets"),
    },
  },
});
