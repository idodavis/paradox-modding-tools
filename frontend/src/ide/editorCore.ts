/**
 * The editor's reading of the backend's answers, free of the workbench: columns between the
 * backend's UTF-8 bytes and the editor's UTF-16 units, a value's name, and the hover card.
 * languageClient.ts hands what these return to the workbench.
 */
import { ValueType, type Value } from "@services/internal/lang/schema";
import type { ImageInfo } from "@services/internal/lang/source";
import type { Inspect, Location, Position, Range } from "@services/internal/lsp";

const encoder = new TextEncoder();
const decoder = new TextDecoder();

/** A line's text by its number; undefined where the file is not open. */
export type LineText = (line: number) => string | undefined;

/** The UTF-8 byte column of a UTF-16 column on a line: what the backend counts. */
export function toBytes(text: string, column: number): number {
  return encoder.encode(text.slice(0, column)).length;
}

/** A backend range in editor columns; a line whose text is unknown keeps its byte columns. */
export function editorRange(range: Range, lineText: LineText): Range {
  const column = (at: Position): Position => {
    const text = lineText(at.line);
    if (text === undefined) return at;
    return { line: at.line, character: decoder.decode(encoder.encode(text).subarray(0, at.character)).length };
  };
  return { start: column(range.start), end: column(range.end) };
}

/** What a word is: a definition's kind, else its value type as Go spells it (scope_path). */
export function valueName(value: Value): string {
  return value.kind || ValueType[value.type].slice(4).replace(/\B(?=[A-Z])/g, "_").toLowerCase();
}

/** What the card needs from the workbench: an origin's name and colour, and a link to a site. */
export interface CardLinks {
  origin(id: string): { name: string; hex: string };
  open(site: Location): string;
}

/** The hover card: what the word is, its prose, where it runs, its facts, where it is defined. */
export function hoverMarkdown(card: Inspect, links: CardLinks): string {
  const code = (text: string) => "`" + text + "`";
  const name = card.value.name || card.key || "";
  const head = [`**${valueName(card.value)}**`, name && code(name), card.class && `· ${card.class}`];
  if (card.key && card.key !== name) head.push(`· at ${code(card.key)}`);
  const parts = [head.filter(Boolean).join(" ")];
  if (card.label) parts.push(card.label.split("\n").map((line) => `> ${line}`).join("\n"));
  // a single newline is markdown's space; two trailing spaces keep an author's line breaks
  if (card.docs) parts.push(card.docs.replace(/(?<!\n)\n(?!\n)/g, "  \n"));
  if (card.usage) parts.push("```\n" + card.usage.trim() + "\n```");
  if (card.accepts?.length) parts.push(`*accepts ${card.accepts.map(code).join(", ")}*`);
  const scope = [card.scope && `in a ${code(card.scope)} scope`, card.scopeOut && `moves to ${code(card.scopeOut)}`];
  if (card.scope || card.scopeOut) parts.push(`*${scope.filter(Boolean).join(" · ")}*`);

  const facts = [card.weight ? `${card.weight}% of its siblings` : "", card.date ?? "", imageFact(card.image)];
  if (card.color && /^#[0-9a-f]{6}$/i.test(card.color)) {
    facts.push(`<span style="color:${card.color};">■</span> ${card.color}`);
  }
  if (facts.some(Boolean)) parts.push(facts.filter(Boolean).join(" · "));
  if (card.values?.length) parts.push(card.values.map((seen) => `${code(seen.text)} ×${seen.count}`).join(" · "));

  // where it is defined, in load order; with more than one, which the game keeps
  const sites = card.sites ?? [];
  const lines = sites.map((site, i) => {
    const origin = links.origin(site.origin);
    const mark = sites.length > 1 && card.winner >= 0 ? (i === card.winner ? " · kept" : " · replaced") : "";
    const at = `${escapeHtml(site.rel)}:${site.range.start.line + 1}`;
    const originLabel = `<span style="color:${origin.hex};">${escapeHtml(origin.name)}</span>`;
    return `${originLabel} · [${at}](${links.open(site)})${mark}`;
  });
  if (lines.length) parts.push("---", lines.join("<br>"));
  return parts.join("\n\n");
}

/** An image file's header as one fact: 512×512 DXT5, 10 mips. */
function imageFact(image: ImageInfo | null | undefined): string {
  if (!image) return "";
  return `${image.width}×${image.height} ${image.format}${image.mips ? `, ${image.mips} mips` : ""}`;
}

function escapeHtml(text: string): string {
  return text.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}
