// The iframe survives window/fullscreen/sidebar size changes. Its document uses
// pixel-sized columns, so it must be laid out again when the actual frame changes.
export function observeEpubViewport(frame: HTMLIFrameElement, layout: () => void) {
  let pending = 0;
  const observer = new ResizeObserver(() => {
    cancelAnimationFrame(pending);
    pending = requestAnimationFrame(layout);
  });
  observer.observe(frame);
  return () => {
    observer.disconnect();
    cancelAnimationFrame(pending);
  };
}
