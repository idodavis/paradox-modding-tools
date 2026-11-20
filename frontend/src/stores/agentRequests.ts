/**
 * What an agent asks the user and how the page answers: a permission for a
 * tool call, a link to open, and a form to fill in.
 */
import type * as v2 from "@agentclientprotocol/sdk/experimental/v2";
import { Browser } from "@wailsio/runtime";
import { lowerFirst } from "es-toolkit";

import type { Tool } from "@services/mcp/models";
import type { Place } from "@services/workspace/models";
import { FindReadOnly } from "@services/workspace/service";
import { loadQuery, placeQuery } from "../queries";
import { fileLabel } from "./edits";

/** A PermissionAnswer is the user's answer to a permission request. */
export type PermissionAnswer = { outcome: { outcome: "cancelled" } | { outcome: "selected"; optionId: string } };

/** A LinkRequest is a link an agent asks the user to open. */
export type LinkRequest = { id: string; message: string; url: string };

/** A Field is one question of a form an agent asks the user to fill in. */
export type Field = {
  key: string;
  title: string;
  description: string;

  /**
   * type is the answer's type as the form's schema names it: string, number,
   * integer, boolean, or array for several choices.
   */
  type: string;

  /** choices are what the user picks from. A typed answer has none. */
  choices: { value: string; title: string; description: string }[];
  isRequired: boolean;
};

/** A FormRequest is a form an agent asks the user to fill in. */
export type FormRequest = { id: string; message: string; fields: Field[] };

/** A Property is one property of a form's schema, of any type. */
type Property = {
  type?: string;
  title?: string | null;
  description?: string | null;
  enum?: string[] | null;
  oneOf?: v2.EnumOption[] | null;
  items?: { enum?: string[] | null; anyOf?: v2.EnumOption[] | null } | null;
};

/** CHANGES holds the kinds of tool call that change files. */
const CHANGES = ["edit", "delete", "move"];

/**
 * toAnswer returns the answer to a permission request that selects an
 * option, or cancels it for null.
 */
export function toAnswer(optionId: string | null): PermissionAnswer {
  return { outcome: optionId ? { outcome: "selected", optionId } : { outcome: "cancelled" } };
}

/**
 * findOption returns the first option of a permission request that has one
 * of the kinds, in their order.
 */
export function findOption(options: v2.PermissionOption[], ...kinds: string[]): v2.PermissionOption | undefined {
  return kinds.flatMap((kind) => options.filter((option) => option.kind === kind))[0];
}

/**
 * describeWish returns a permission prompt for a tool of PMT's: which agent
 * wants to do what, and to which file if the call names one.
 */
export function describeWish(agent: string, tool: Tool, toolCall: v2.ToolCallUpdate | undefined): string {
  const path = findPath(toolCall);

  return `${agent} wants to ${lowerFirst(tool.title)}${path ? `: ${fileLabel(path)}` : ""}`;
}

/**
 * listChanged returns the places of the files a tool call would change, and
 * none for a tool call that changes no file.
 */
export async function listChanged(toolCall: v2.ToolCallUpdate | undefined): Promise<Place[]> {
  if (!toolCall?.kind || !CHANGES.includes(toolCall.kind)) return [];
  const places = await Promise.all(listPaths(toolCall).map((path) => loadQuery(placeQuery(path))));

  return places.filter((place) => !!place);
}

/**
 * findNamed returns the place of the read-only folder that a shell command
 * names, the game's or an external mod's, or undefined if the tool call is no
 * shell command or names none. No path rule of an agent covers its shell, so
 * the user is told.
 */
export async function findNamed(toolCall: v2.ToolCallUpdate | undefined): Promise<Place | undefined> {
  if (toolCall?.kind !== "execute") return undefined;
  const input = toolCall.rawInput as { command?: unknown } | null | undefined;
  const command = typeof input?.command === "string" ? input.command : "";
  const dir = await FindReadOnly(`${toolCall.title ?? ""}\n${command}`);

  return dir ? ((await loadQuery(placeQuery(dir))) ?? undefined) : undefined;
}

/**
 * toLink returns the link an agent's request asks the user to open. It returns
 * null if the request asks for a form.
 */
export function toLink(request: v2.CreateElicitationRequest): LinkRequest | null {
  if (request.mode !== "url" || !("url" in request)) return null;

  return { id: String(request.elicitationId), message: request.message, url: String(request.url) };
}

/**
 * answerLink returns the answer to a request to open a link, and opens the
 * link first if the user accepted. Any answer but accept or decline cancels
 * the request.
 */
export function answerLink(url: string, action: string | null): v2.CreateElicitationResponse {
  if (action === "accept") void Browser.OpenURL(url);

  const choice = action === "accept" || action === "decline" ? action : "cancel";

  return { action: choice } as v2.CreateElicitationResponse;
}

/**
 * toForm returns the form an agent's request asks the user to fill in, under
 * the given id, with its questions in the schema's order. It returns null if
 * the request asks for no form.
 *
 * A question with choices is a string that lists them, or an array whose
 * items list them, where several can be picked:
 *
 *	{ "type": "string", "oneOf": [{ "const": "Red", "title": "Red" }] }
 */
export function toForm(request: v2.CreateElicitationRequest, id: string): FormRequest | null {
  if (request.mode !== "form" || !("requestedSchema" in request)) return null;
  const schema = request.requestedSchema as v2.ElicitationSchema;
  const fields = Object.entries(schema.properties as Record<string, Property>).map(([key, property]) => {
    const plain = (property.enum ?? property.items?.enum ?? []).map((value) => ({ const: value, title: value }));
    const listed: v2.EnumOption[] = [...(property.oneOf ?? property.items?.anyOf ?? []), ...plain];

    return {
      key,
      title: property.title ?? key,
      description: property.description ?? "",
      type: property.type ?? "string",
      choices: listed.map((choice) => ({
        value: choice.const,
        title: choice.title,
        description: choice.description ?? "",
      })),
      isRequired: !!schema.required?.includes(key),
    };
  });

  return { id, message: request.message, fields };
}

/**
 * listPaths returns the files a tool call names, by its locations and its
 * input, each once.
 */
function listPaths(toolCall: v2.ToolCallUpdate | undefined): string[] {
  const paths = new Set(toolCall?.locations?.map((location) => location.path));
  const input = toolCall?.rawInput as { path?: unknown; file_path?: unknown } | null | undefined;
  for (const path of [input?.path, input?.file_path]) if (typeof path === "string") paths.add(path);

  return [...paths];
}

/**
 * findPath returns the one file a tool call names, or "" if it names none or
 * many.
 */
function findPath(toolCall: v2.ToolCallUpdate | undefined): string {
  const paths = listPaths(toolCall);

  return paths.length === 1 ? (paths[0] ?? "") : "";
}
