import { useEffect, useRef, useState } from "react";
import { request } from "./api";
import { invalidateVideoPoster } from "./video-poster-cache";
import { message, type Metadata, type Job } from "./video-library-types";

export function VideoMetadataEditor({ videoId, zh, onClose, onChange }: { videoId: number; zh: boolean; onClose: () => void; onChange: () => void }) {
  const t = (en: string, cn: string) => zh ? cn : en;
  const [meta, setMeta] = useState<Metadata | null>(null); const [error, setError] = useState(""); const [busy, setBusy] = useState(false);
  const [title, setTitle] = useState(""); const [year, setYear] = useState("0"); const [overview, setOverview] = useState("");
  const [matchTitle, setMatchTitle] = useState(""); const [kind, setKind] = useState("movie"); const [season, setSeason] = useState("1"); const [episode, setEpisode] = useState("1"); const [version, setVersion] = useState("");
  const [workId, setWorkId] = useState(""); const [job, setJob] = useState<Job | null>(null); const [candidateJob, setCandidateJob] = useState<Job | null>(null); const [enabled, setEnabled] = useState(false);
  const base = `/api/videos/${videoId}`;
  const editor = useRef<HTMLElement>(null);
  useEffect(() => { editor.current?.focus(); editor.current?.scrollIntoView({ block: "start" }); }, []);
  useEffect(() => {
    const abort = new AbortController();
    Promise.all([request<Metadata>(`${base}/metadata`, { signal: abort.signal }), request<{ enabled: boolean }>("/api/videos/metadata/provider", { signal: abort.signal }), request<Job>(`${base}/metadata/candidates`, {signal: abort.signal}).catch(() => null)])
      .then(([m, p, j]) => { if (abort.signal.aborted) return; setMeta(m); setTitle(m.title); setYear(String(m.year)); setOverview(m.overview); setMatchTitle(m.showTitle || m.title); setKind(m.kind === "episode" ? "series" : "movie"); setSeason(String(m.season ?? 1)); setEpisode(String(m.episode ?? 1)); setEnabled(p.enabled); if(j){setJob(j);if(j.status === "ready" && j.candidates.length)setCandidateJob(j);} })
      .catch(e => { if (!abort.signal.aborted) setError(message(e)); });
    return () => abort.abort();
  }, [base]);
  useEffect(() => {
    if (!job || !["queued", "running"].includes(job.status)) return;
    const abort = new AbortController();
    const timer = window.setTimeout(() => { request<Job>(`/api/video-metadata-jobs/${job.id}`, { signal: abort.signal }).then(j => { if (abort.signal.aborted) return; setJob(j); if (j.status === "ready" && j.candidates.length) setCandidateJob(j); if (!["queued", "running"].includes(j.status)) { void request<Metadata>(`${base}/metadata`, { signal: abort.signal }).then(setMeta).catch(() => {}); invalidateVideoPoster(videoId); onChange(); } }).catch(e => { if (!abort.signal.aborted) setError(message(e)); }); }, 1500);
    return () => { abort.abort(); window.clearTimeout(timer); };
  }, [job, base, onChange]);
  async function action(fn: () => Promise<unknown>) {
    setBusy(true); setError("");
    try { await fn(); invalidateVideoPoster(videoId); onChange(); setMeta(await request<Metadata>(`${base}/metadata`)); }
    catch (e) { setError(message(e)); } finally { setBusy(false); }
  }
  function matchBody() { return { title: matchTitle, kind, year: Number(year), overview, workId: Number(workId || 0), generation: meta?.generation, version, ...(kind === "series" ? { season: Number(season), episode: Number(episode) } : {}) }; }
  async function saveField(field: string, value: string | number) { await request(`${base}/metadata`, { method: "PUT", body: JSON.stringify({ field, value }) }); }
  return <section ref={editor} tabIndex={-1} className="videoEditor" aria-label={t("Metadata editor", "元数据编辑")}>
    <header><h3>{t("Manage file", "管理文件")} #{videoId}</h3><button disabled={busy} onClick={onClose}>{t("Close", "关闭")}</button></header>
    {error ? <p role="alert">{error}</p> : null}
    {!meta ? <p role="status">{t("Loading…", "正在加载…")}</p> : <>
      <p>{t("Match status", "关联状态")}: {meta.status} {meta.warning ? `· ${meta.warning}` : ""}</p>
      <div className="videoEditFields">
        <label>{t("File title", "文件显示标题")}<input value={title} onChange={e => setTitle(e.target.value)} /><button disabled={busy} onClick={() => void action(() => saveField("title", title))}>{t("Save & protect", "保存并保护")}</button></label>
        <label>{t("Year", "年份")}<input type="number" min="0" max="9999" value={year} onChange={e => setYear(e.target.value)} /><button disabled={busy} onClick={() => void action(() => saveField("year", Number(year)))}>{t("Save & protect", "保存并保护")}</button></label>
        <label>{t("Overview", "简介")}<textarea value={overview} onChange={e => setOverview(e.target.value)} /><button disabled={busy} onClick={() => void action(() => saveField("overview", overview))}>{t("Save & protect", "保存并保护")}</button></label>
      </div>
      <div>{t("Protected fields", "受保护字段")}: {meta.lockedFields.join(", ") || "—"} {meta.lockedFields.map(field => <button key={field} disabled={busy} onClick={() => void action(async () => { const m = await request<Metadata>(`${base}/metadata`, { method: "DELETE", body: JSON.stringify({ field }) }); setTitle(m.title); setYear(String(m.year)); setOverview(m.overview); })}>{t("Reset", "恢复自动值")} {field}</button>)}</div>
      <form className="videoEditFields" onSubmit={e => { e.preventDefault(); void action(() => request(`${base}/metadata/match`, { method: "POST", body: JSON.stringify(matchBody()) })); }}>
        <h4>{t("Associate work / version", "关联作品 / 版本")}</h4>
        <label>{t("Work title / search", "作品名 / 搜索词")}<input required value={matchTitle} onChange={e => setMatchTitle(e.target.value)} /></label>
        <label>{t("Type", "类型")}<select value={kind} onChange={e => setKind(e.target.value)}><option value="movie">{t("Movie", "电影")}</option><option value="series">{t("Series", "剧集")}</option></select></label>
        <label>{t("Existing work ID (optional)", "已有作品 ID（可选）")}<input type="number" min="1" value={workId} onChange={e => setWorkId(e.target.value)} /></label>
        <label>{t("Version label", "版本名")}<input placeholder={t("e.g. Director cut · 4K", "例如 导演剪辑版 · 4K")} value={version} onChange={e => setVersion(e.target.value)} /></label>
        {kind === "series" ? <><label>{t("Season", "季")}<input required type="number" min="0" value={season} onChange={e => setSeason(e.target.value)} /></label><label>{t("Episode", "集")}<input required type="number" min="0" value={episode} onChange={e => setEpisode(e.target.value)} /></label></> : null}
        <button disabled={busy}>{t("Confirm association", "确认关联")}</button>
        <button type="button" disabled={busy || !meta.workId} onClick={() => { if (window.confirm(t("Unlink this file? Rescans will not automatically link it again.", "解除此文件关联？重扫不会自动关联回来。"))) void action(() => request(`${base}/metadata/unlink`, { method: "POST", body: JSON.stringify({ generation: meta.generation }) })); }}>{t("Unlink", "解除关联")}</button>
      </form>
      <div className="videoProviderPanel"><h4>TMDB</h4>
        <p>{enabled ? t("Results are suggestions. Confirm the correct title before applying.", "搜索结果仅是候选，请确认正确作品后采用。") : t("Disabled. Configure the server provider and token to use online matching. Local metadata works without it.", "未启用。服务器配置提供方和 token 后可在线匹配；本地元数据不受影响。")}</p>
        <button disabled={busy || !enabled || ["queued", "running"].includes(job?.status || "")} onClick={() => void action(async () => { setJob(await request<Job>(`${base}/metadata/refresh`, { method: "POST", body: JSON.stringify({ query: matchTitle, kind }) })); })}>{t("Find candidates", "查找候选")}</button>
        {job ? <p role="status">{job.status} {job.errorCode || ""} {job.nextAt > Date.now() / 1000 ? t("Waiting to retry", "等待重试") : ""} {["queued", "running"].includes(job.status) ? <button onClick={() => void action(async () => setJob(await request<Job>(`/api/video-metadata-jobs/${job.id}`, { method: "DELETE" })))}>{t("Cancel task", "取消任务")}</button> : null}</p> : null}
        {job?.status === "ready" && job.candidates.length === 0 && !candidateJob ? <p>{t("No candidates found. Use manual association.", "未找到候选，可手动关联。")}</p> : null}
        {candidateJob?.candidates.map((c, index) => <article key={c.id}><strong>{c.title} ({c.year || "—"})</strong><p>{c.overview}</p><button disabled={busy} onClick={() => void action(() => request(`${base}/metadata/select-match`, { method: "POST", body: JSON.stringify({ ...matchBody(), jobId: candidateJob.id, candidate: index }) }))}>{t("Use this match", "采用此候选")}</button>
          {(["poster", "backdrop"] as const).map(role => (role === "poster" ? c.posterPath : c.backdropPath) ? <button key={role} disabled={busy} onClick={() => void action(async () => setJob(await request<Job>(`${base}/artwork/select`, { method: "POST", body: JSON.stringify({ jobId: candidateJob.id, candidate: index, role }) })))}>{role === "poster" ? t("Use poster", "采用海报") : t("Use backdrop", "采用背景图")}</button> : null)}</article>)}
      </div>
      <div className="videoEditFields"><h4>{t("Manual artwork", "手动图片")}</h4>{(["poster", "backdrop"] as const).map(role => <label key={role}>{role === "poster" ? t("Poster", "海报") : t("Backdrop", "背景图")}<input type="file" accept="image/jpeg,image/png,image/webp" disabled={busy} onChange={e => { const f = e.target.files?.[0]; if (f) void action(() => request(`${base}/artwork/${role}?generation=${meta.generation}`, { method: "PUT", headers: { "Content-Type": f.type }, body: f })); e.target.value = ""; }} /><button disabled={busy} onClick={() => void action(() => request(`${base}/artwork/${role}`, { method: "DELETE" }))}>{t("Reset to local image", "恢复本地图片")}</button></label>)}</div>
    </>}
  </section>;
}
