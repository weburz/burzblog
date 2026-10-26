// How far a scroll container must move so the caret sits inside the visible
// band, with breathing room above (a sticky toolbar) and below. Zero when it
// is already comfortably in view. All values share one coordinate space.
export const caretScroll = (
  caretTop: number,
  caretBottom: number,
  viewTop: number,
  viewBottom: number,
  margin: number,
): number => {
  if (caretBottom > viewBottom - margin) return caretBottom - (viewBottom - margin);
  if (caretTop < viewTop + margin) return caretTop - (viewTop + margin);
  return 0;
};
