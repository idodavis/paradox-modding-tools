/**
 * The keys that step a history back and forth, read one way for every place that keeps one: the explorer's moves, and
 * a file's text in the Visual view.
 */

/** A history a key steps: what takes its last step back, and what makes its last undone step again. */
type History = { undo: () => unknown; redo: () => unknown };

/** Steps a history as a key asks, the key then used up: back for Ctrl Z, again for Ctrl Y or Ctrl Shift Z; nothing for
 * any other key, or in a text input, which keeps its own. Reports whether the key stepped it. */
export function stepHistory(event: KeyboardEvent, history: History): boolean {
  const key = event.key.toLowerCase();
  const isTyping = event
    .composedPath()
    .some((node) => node instanceof HTMLInputElement || node instanceof HTMLTextAreaElement);
  if (!event.ctrlKey || isTyping || (key !== "z" && key !== "y")) return false;
  event.preventDefault();
  void (key === "y" || event.shiftKey ? history.redo() : history.undo());
  return true;
}
