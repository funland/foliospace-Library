type EpubLayoutOptions = {
  viewportWidth: number;
  pageWidth: number;
  pagePosition: number;
  target?: Element | null;
};

// CSS transforms affect scrollable overflow in the iframe. Measuring while on
// a later page would shrink the apparent book and clamp navigation backwards.
// Keep measurement and placement synchronous so the zero position is not painted.
export function placeEpubPage(doc: Document, options: EpubLayoutOptions) {
  const body = doc.body;
  const style = body.style;
  const previousTransform = style.getPropertyValue("transform");
  const transformPriority = style.getPropertyPriority("transform");
  const previousTransition = style.getPropertyValue("transition");
  const transitionPriority = style.getPropertyPriority("transition");
  let placed = false;
  const restoreProperty = (name: string, value: string, priority: string) => {
    if (value) style.setProperty(name, value, priority);
    else style.removeProperty(name);
  };
  try {
    style.setProperty("transition", "none", "important");
    style.setProperty("transform", "none", "important");
    const width = Math.max(body.scrollWidth, doc.documentElement.scrollWidth, options.viewportWidth);
    // The viewport includes side padding and can be wider than the page step.
    // Count the shifts needed to reveal the final extent, not padding as a page.
    const pageCount = 1 + Math.ceil(Math.max(0, width - options.viewportWidth) / options.pageWidth);
    const targetRect = options.target?.getClientRects()[0] ?? options.target?.getBoundingClientRect();
    const requestedPosition = targetRect
      ? Math.floor(Math.max(0, targetRect.left - body.getBoundingClientRect().left) / options.pageWidth)
      : options.pagePosition;
    const pagePosition = Math.max(0, Math.min(requestedPosition, pageCount - 1));
    style.setProperty("transform", `translateX(-${pagePosition * options.pageWidth}px)`, "important");
    // Commit placement while transitions are disabled, before restoring the
    // author's/reader's transition declaration. No intermediate slide from zero.
    body.getBoundingClientRect();
    placed = true;
    return { pageCount, pagePosition };
  } finally {
    if (!placed) restoreProperty("transform", previousTransform, transformPriority);
    restoreProperty("transition", previousTransition, transitionPriority);
  }
}
