// Client-session invalidation only: ordinary navigation keeps a stable URL;
// successful edits must not resurrect an earlier in-document image on return.
const edits = new Map<number, number>();
let reset = 0;

export function invalidateAllVideoPosters() {
  reset += 1;
}

export function invalidateVideoPoster(videoId: number) {
  edits.set(videoId, (edits.get(videoId) ?? 0) + 1);
}

export function videoPosterSource(videoId: number, revision = 0) {
  const edit = edits.get(videoId) ?? 0;
  return `/api/videos/${videoId}/artwork/poster?r=${revision}${edit ? `&edit=${edit}` : ""}${reset ? `&reset=${reset}` : ""}`;
}
