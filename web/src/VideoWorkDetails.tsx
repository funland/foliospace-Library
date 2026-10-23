import { useEffect, useState, useRef } from "react";
import { request } from "./api";
import { message, type Work, type WorkDetails } from "./video-library-types";
import { videoProgressLabel } from "./video-details";

export function VideoWorkDetails({ work, zh }: { work: Work; zh: boolean }) {
 const generation = useRef(0);
 const refreshAbort = useRef<AbortController | null>(null);
 const [details, setDetails] = useState<WorkDetails>({});
 const [enabled, setEnabled] = useState(false);
 const [loading, setLoading] = useState(false);
 const [error, setError] = useState("");
 const text = (en: string, cn: string) => zh ? cn : en;
 useEffect(() => {
  const abort = new AbortController(); const version = ++generation.current;
  request<WorkDetails>(`/api/client/video-works/${work.id}/details`, { signal: abort.signal }).then(value => {if (!abort.signal.aborted && generation.current === version) setDetails(value);}).catch(e => { if (!abort.signal.aborted && generation.current === version) setError(message(e)); });
  if (work.provider === "tmdb") request<{enabled: boolean}>("/api/videos/metadata/provider", { signal: abort.signal }).then(s => {if (!abort.signal.aborted) setEnabled(s.enabled);}).catch(() => {});
  return () => {abort.abort();refreshAbort.current?.abort();generation.current++;};
 }, [work.id, work.provider]);
 async function load() {
  const version = ++generation.current; const abort = new AbortController(); refreshAbort.current?.abort();refreshAbort.current=abort;
  setLoading(true);setError("");
  try { const value = await request<WorkDetails>(`/api/client/video-works/${work.id}/details`,{ method: "POST", signal: abort.signal }); if (!abort.signal.aborted && generation.current === version) setDetails(value); }
  catch(e) { if (!abort.signal.aborted && generation.current === version) setError(text("Could not fetch details. Existing metadata is preserved. ", "获取资料失败，已有元数据仍保留。 ") + message(e)); }
  finally {if (!abort.signal.aborted && generation.current === version) setLoading(false);}
 }
 const rows: [string,string][] = [];
 if (work.year) rows.push([text("Year","年份"),String(work.year)]);
 if (details.originalTitle && details.originalTitle !== work.title) rows.push([text("Original title","原名"),details.originalTitle]);
 if (details.runtimeMinutes) rows.push([text("Runtime","片长"),`${details.runtimeMinutes} ${text("min","分钟")}`]);
 if (details.genres?.length) rows.push([text("Genres","类型"),details.genres.join(" · ")]);
 if (details.directors?.length) rows.push([work.kind === "series" ? text("Directors / creators","导演 / 主创") : text("Directors","导演"),details.directors.join(" / ")]);
 if (details.cast?.length) rows.push([text("Cast","演员"),details.cast.join(" / ")]);
 if (details.voteCount && details.rating !== undefined) rows.push([text("TMDB rating","TMDB 评分"),`${details.rating.toFixed(1)} / 10 · ${details.voteCount} ${text("votes","人评分")}`]);
 return <div className="videoWorkDetails">
  <dl>{rows.map(([label,value])=><div key={label}><dt>{label}</dt><dd>{value}</dd></div>)}</dl>
  {work.fileCount ? <p>{work.fileCount} {text("files","个文件")} · {work.versionCount || 0} {text("versions","个版本")}{work.kind === "series" ? ` · ${work.availableEpisodeCount || 0} ${text("available episodes","集可观看")}` : ""}</p> : null}
  {enabled ? <button disabled={loading} onClick={() => void load()}>{loading ? text("Fetching…","正在获取…") : details.fetchedAt ? text("Refresh TMDB details","刷新 TMDB 资料") : text("Fetch TMDB details","获取 TMDB 作品资料")}</button> : null}
  {details.fetchedAt ? <small>{text("TMDB details updated","TMDB 资料更新于")} {new Date(details.fetchedAt * 1000).toLocaleDateString(zh ? "zh-CN" : "en-US")}</small> : null}
  {error ? <p role="alert">{error}</p> : null}
 </div>;
}
export function VideoSavedProgress({ id, zh }: { id: number; zh: boolean }) {
 const [label,setLabel] = useState("");
 useEffect(()=>{const abort=new AbortController();request<{completed:boolean;positionSeconds:number;durationSeconds:number;identityChanged?:boolean}>(`/api/client/videos/${id}/playback-state`,{signal:abort.signal}).then(s=>{if(!abort.signal.aborted)setLabel(videoProgressLabel(s,zh));}).catch(()=>{});return()=>abort.abort();},[id,zh]);
 return label ? <small>{label}</small> : null;
}
