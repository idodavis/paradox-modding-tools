// The stores read this window's name from the page's URL; the tests are the main window.
import { vi } from "vitest";

vi.stubGlobal("location", new URL("http://localhost/?window=main"));
