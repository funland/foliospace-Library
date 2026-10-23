import { useCallback, useEffect, useState } from "react";
import { request, type VideoAsset } from "./api";
import { VideoMetadataEditor } from "./VideoMetadataEditor";
import { VideoPoster } from "./VideoPoster";
import { invalidateAllVideoPosters } from "./video-poster-cache";
import { message, type Page, type Work, type WorkFile } from "./video-library-types";
import "./video-library.css";
import { VideoWorkDetails, VideoSavedProgress } from "./VideoWorkDetails";
import { videoSpecs } from "./video-details";

export function VideoLibrary({ locale, onPlay, presentation = "catalog" }: { locale: string; onPlay: (video: VideoAsset) => void; presentation?: "home" | "catalog" }) {
  const zh = locale.startsWith("zh");
  const text = (en: string, cn: string) => zh ? cn : en;
  const [tab, setTab] = useState<"works" | "files">("works");
  const [query, setQuery] = useState(""); const [search, setSearch] = useState("");
  const [kind, setKind] = useState(""); const [offset, setOffset] = useState(0);
  const [revision, setRevision] = useState(0); const [work, setWork] = useState<Work | null>(null);
  const [season, setSeason] = useState(""); const [editing, setEditing] = useState<number | null>(null);
  const [page, setPage] = useState<Page<Work | VideoAsset | WorkFile> | null>(null);
  const [error, setError] = useState(""); const [loading, setLoading] = useState(false);
  const changed = useCallback(() => setRevision(v => v + 1), []);
  useEffect(() => {
    const abort = new AbortController(); setLoading(true); setError(""); setPage(null);
    const params = new URLSearchParams({ limit: "24", offset: String(offset), q: search, kind });
    if (season !== "") params.set("season", season);
    const base = work ? `/api/client/video-works/${work.id}/files` : tab === "works" ? "/api/client/video-works" : "/api/client/videos";
    request<Page<Work | VideoAsset | WorkFile>>(`${base}?${params}`, { signal: abort.signal })
      .then(value => { if (!abort.signal.aborted) setPage(value); })
      .catch(e => { if (!abort.signal.aborted) setError(message(e)); })
      .finally(() => { if (!abort.signal.aborted) setLoading(false); });
    return () => abort.abort();
  }, [tab, search, kind, offset, work, season, revision]);
  useEffect(() => {
    if (!work || revision === 0) return;
    const abort = new AbortController();
    request<Work>(`/api/client/video-works/${work.id}`,{signal:abort.signal}).then(value=>{if(!abort.signal.aborted)setWork(value);}).catch(e=>{if(!abort.signal.aborted)setError(message(e));});
    return () => abort.abort();
  }, [work?.id, revision]);
  async function play(id: number) {
    try { const result = await request<{ video: VideoAsset }>(`/api/client/videos/${id}/manifest`); onPlay(result.video); }
    catch (e) { setError(message(e)); }
  }
  return <section className={`videoLibrary${presentation === "home" ? " videoHomeLibrary" : ""}`} aria-label={text("Movie and TV library", "影视资料库")}>
    {work ? <img className="videoBackdrop" src={`/api/videos/${work.videoId}/artwork/backdrop?r=${revision}`} alt="" /> : null}
    <header><div><small>FOLIOSPACE LIBRARY</small><h2>{work?.title || (presentation === "home" ? text("Collections · Movies & series", "作品集 · 电影与剧集") : text("Movies & series", "电影与剧集"))}</h2>
      <p>{(work ? work.overview || text("No synopsis available.", "暂无剧情简介。") : text("Browse by title, then choose a version or episode. Unmatched videos are available in All files.", "按作品浏览海报，点开选择版本或剧集；未匹配影片仍可在全部文件中访问。"))}</p></div>
      {work ? <button onClick={() => { setWork(null); setOffset(0); setSeason(""); }}>{text("Back to library", "返回资料库")}</button> : null}
    </header>
    {work ? <VideoWorkDetails key={`${work.id}:${revision}`} work={work} zh={zh} /> : null}
    {!work ? <form className="videoLibraryTools" onSubmit={e => { e.preventDefault(); setSearch(query); setOffset(0); }}>
      <div role="group" aria-label={text("Browse mode", "浏览方式")}>
        <button type="button" aria-pressed={tab === "works"} onClick={() => { setTab("works"); setOffset(0); }}>{text("Poster wall", "海报墙")}</button>
        <button type="button" aria-pressed={tab === "files"} onClick={() => { setTab("files"); setOffset(0); }}>{text("All files / unmatched", "全部文件 / 未匹配")}</button>
      </div>
      <label>{text("Search", "搜索")}<input value={query} onChange={e => setQuery(e.target.value)} /></label>
      {tab === "works" ? <label>{text("Type", "类型")}<select value={kind} onChange={e => { setKind(e.target.value); setOffset(0); }}>
        <option value="">{text("All", "全部")}</option><option value="movie">{text("Movies", "电影")}</option><option value="series">{text("Series", "剧集")}</option>
      </select></label> : null}<button>{text("Search", "搜索")}</button>
    </form> : work.kind === "series" ? <label>{text("Season (empty = all)", "季数（留空显示全部）")}<input type="number" min="0" value={season} onChange={e => { setSeason(e.target.value); setOffset(0); }} /></label> : null}
    <div className="videoWallHeading"><h3>{work ? text("Versions & episodes", "版本与剧集") : tab === "works" ? text("Cover wall", "封面墙") : text("All video files", "全部影片文件")}</h3>
      <small aria-live="polite">{page ? `${page.total} ${work || tab === "files" ? text("files", "个文件") : text("works", "部作品")}` : ""}</small>
    </div>
    {error ? <p role="alert">{error} <button onClick={() => setRevision(v => v + 1)}>{text("Retry", "重试")}</button></p> : null}
    {loading ? <p role="status">{text("Loading…", "正在加载…")}</p> : null}
    {!loading && page?.items.length === 0 ? <p className="videoEmpty">{text("No items here yet. Scan your library, or use All files to match a title manually. Unmatched files remain available.", "暂无作品。请扫描资料库，或到全部文件中手动匹配；未匹配的文件仍可访问。")}</p> : null}
    <div className={tab === "works" && !work ? "videoPosterGrid" : "videoFileList"}>
      {page?.items.map(item => {
        if (!work && tab === "works") { const w = item as Work; return <button className="videoPosterCard" key={w.id} onClick={() => { setWork(w); setOffset(0); }}>
          <VideoPoster videoId={w.videoId} revision={revision} /><strong>{w.title}</strong><small>{w.year || "—"} · {w.kind === "series" ? text("Series", "剧集") : text("Movie", "电影")}</small>
        </button>; }
        const f = item as WorkFile; const v = item as VideoAsset; const id = work ? f.videoId : v.id;
        return <article key={id}><VideoPoster videoId={id} revision={revision} /><div><strong>{work ? f.title : v.displayTitle || v.title}</strong>
          {work ? <small>{f.season !== null ? `${text("Season", "第")} ${f.season} ${text("Episode", "季 · 第")} ${f.episode} ${text("", "集")} · ` : ""}{/^\w+ · file \d+$/.test(f.version) ? `${text("Original version", "原始版本")} ${f.videoId}` : f.version}</small> : null}
          <small>{videoSpecs(work ? f : v, zh).join(" · ")}</small>
          <VideoSavedProgress key={id} id={id} zh={zh} />
          <button onClick={() => void play(id)}>{text("Play", "播放")}</button> <a href={`/api/client/videos/${id}/file`}>{text("Original file", "原文件")}</a> <button onClick={() => setEditing(id)}>{text("Manage metadata", "管理元数据")}</button>
        </div></article>;
      })}
    </div>
    <nav className="videoPagination" aria-label={text("Pagination", "分页")}>
      <button disabled={loading || offset === 0} onClick={() => setOffset(Math.max(0, offset - 24))}>{text("Previous", "上一页")}</button>
      <span>{page ? `${page.total ? offset + 1 : 0}–${offset + page.items.length} / ${page.total}` : "—"}</span>
      <button disabled={loading || !page?.hasMore} onClick={() => setOffset(offset + 24)}>{text("Next", "下一页")}</button>
    </nav>
    {editing !== null ? <VideoMetadataEditor key={editing} videoId={editing} zh={zh} onClose={() => setEditing(null)} onChange={changed} /> : null}
    <footer><a href="https://www.themoviedb.org" target="_blank" rel="noreferrer"><img src="/tmdb.svg" width="130" height="32" alt="The Movie Database (TMDB)" /></a><p>This product uses the TMDB API but is not endorsed or certified by TMDB.</p><small>{text("Online matching is optional. Bring your own TMDB read token; commercial permission is separate. No movies are provided or downloaded by FolioSpace.", "在线匹配为可选功能，需自备 TMDB 读取令牌；商业授权需另行核实。FolioSpace 不提供或下载影片。")}</small>
      {presentation === "catalog" ? <p><button onClick={async () => { if (!window.confirm(text("Clear all TMDB metadata, artwork and candidates? Your files, local sidecars and manual fields will be kept.", "清除所有 TMDB 元数据、图片和候选？保留原文件、本地附属文件及人工字段。"))) return; try { await request("/api/videos/metadata/provider", { method: "DELETE" }); invalidateAllVideoPosters(); setWork(null); changed(); } catch(e) { setError(message(e)); } }}>{text("Clear TMDB cache", "清除 TMDB 缓存")}</button></p> : null}
    </footer>
  </section>;
}
