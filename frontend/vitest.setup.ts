// The stores read this window's name from the page's URL; the tests are the main window.
import { vi } from "vitest";

vi.stubGlobal("location", new URL("http://localhost/?window=main"));
// Nuxt UI's composables are auto-imported by its plugin, which the tests do not load
vi.stubGlobal("useToast", () => ({ add: vi.fn() }));
vi.stubGlobal("useOverlay", () => ({ create: () => ({ open: vi.fn() }) }));
