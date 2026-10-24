// Inspect DOM shape rather than instanceof: EPUB targets belong to another window.
export function readerKeyboardAction(event: KeyboardEvent): "previous" | "next" | "fullscreen" | null {
  if (event.defaultPrevented || event.isComposing || event.altKey || event.ctrlKey || event.metaKey || event.shiftKey) return null;
  const target = event.target as HTMLElement | null;
  if (target?.isContentEditable || target?.closest?.('input, textarea, select, [contenteditable]:not([contenteditable="false"]), [role="textbox"], [role="slider"], [role="combobox"]')) return null;
  if (event.key === "ArrowLeft") return "previous";
  if (event.key === "ArrowRight") return "next";
  if (event.key.toLowerCase() === "f") return "fullscreen";
  return null;
}

export function bindReaderKeyboard(target: EventTarget, handler: (event: KeyboardEvent) => void) {
  const listener = (event: Event) => handler(event as KeyboardEvent);
  target.addEventListener("keydown", listener);
  return () => target.removeEventListener("keydown", listener);
}
