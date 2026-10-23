export type Page<T> = { items: T[]; total: number; limit: number; offset: number; hasMore: boolean };
export type Work = { id: number; title: string; kind: "movie" | "series"; year: number; overview: string; fileCount?: number; versionCount?: number; availableEpisodeCount?: number; videoId: number; posterUrl: string; provider?: string };
export type WorkFile = { videoId: number; title: string; version: string; season: number | null; episode: number | null; fileUrl: string; format: string; size: number; durationSeconds: number; width: number; height: number; videoCodec?: string; audioCodec?: string };
export type Metadata = { title: string; overview: string; year: number; kind: string; showTitle?: string; season?: number; episode?: number; generation: number; status: string; workId?: number; lockedFields: string[]; warning?: string };
export type Candidate = { id: string; title: string; kind: string; year: number; overview: string; posterPath?: string; backdropPath?: string };
export type Job = { id: number; videoId: number; status: string; errorCode?: string; candidates: Candidate[]; nextAt: number; generation: number };
export function message(error: unknown): string {
  if (!(error instanceof Error)) return String(error);
  try { const body = JSON.parse(error.message); return body.code || body.error || error.message; } catch { return error.message; }
}

export type WorkDetails = { originalTitle?: string; runtimeMinutes?: number; genres?: string[]; directors?: string[]; cast?: string[]; rating?: number; voteCount?: number; fetchedAt?: number };
