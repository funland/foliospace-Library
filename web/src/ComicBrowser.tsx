import { useEffect, useState, type ReactNode } from "react";
import { request, type Library, type Series } from "./api";
import "./comic-browser.css";
import { collectionCountScope, collectionDisplayTitle, displayText, originalCollectionPath } from "./library-presentation";

type Node = Partial<Series> & { kind: "folder" | "collection"; libraryId: number; path: string; title: string; displayTitle: string; bookCount: number; collectionCount?: number };
type Page = { items: Node[]; total: number; hasMore: boolean };
type Rule = { pattern: string; replacement: string };
type Rules = { rules: Rule[]; defaults: Rule[]; samples: { id: number; path: string; original: string; displayTitle: string }[] };
const copy = {
  zh: { title: "漫画目录", all: "全部资源库", root: "漫画", search: "搜索漫画", go: "搜索", loading: "正在加载…", empty: "此目录暂无漫画", prev: "上一页", next: "下一页", books: "本", collections: "个作品集", original: "原始路径", rules: "漫画标题清洗", hint: "只调整显示名称，原文件、目录和收藏不变。按顺序应用规则；保留作者、卷号等有用信息。", pattern: "匹配规则（正则表达式）", replacement: "替换为（可留空）", add: "添加规则", remove: "移除", preview: "预览", save: "保存规则", reset: "恢复默认规则", saved: "已保存", samples: "名称预览", noSamples: "扫描资源库后可预览名称", directory: "目录" },
  zht: { title: "漫畫目錄", all: "全部資源庫", root: "漫畫", search: "搜尋漫畫", go: "搜尋", loading: "載入中…", empty: "此目錄暫無漫畫", prev: "上一頁", next: "下一頁", books: "本", collections: "個作品集", original: "原始路徑", rules: "漫畫標題清理", hint: "只調整顯示名稱，原檔案、目錄和收藏不變。依序套用規則，保留作者、卷號等資訊。", pattern: "比對規則（正規表示式）", replacement: "替換為（可留空）", add: "新增規則", remove: "移除", preview: "預覽", save: "儲存規則", reset: "恢復預設規則", saved: "已儲存", samples: "名稱預覽", noSamples: "掃描資源庫後可預覽名稱", directory: "目錄" },
  en: { title: "Comic folders", all: "All libraries", root: "Comics", search: "Search comics", go: "Search", loading: "Loading…", empty: "No comics in this folder", prev: "Previous", next: "Next", books: "volumes", collections: "collections", original: "Original path", rules: "Comic title cleanup", hint: "Only display names change. Files, folders and favorites stay intact. Rules run in order; keep useful author and volume information.", pattern: "Match pattern (regular expression)", replacement: "Replacement (can be empty)", add: "Add rule", remove: "Remove", preview: "Preview", save: "Save rules", reset: "Restore defaults", saved: "Saved", samples: "Name preview", noSamples: "Scan this library to preview names", directory: "Folder" },
  ja: { title: "漫画フォルダ", all: "すべてのライブラリ", root: "漫画", search: "漫画を検索", go: "検索", loading: "読み込み中…", empty: "このフォルダには漫画がありません", prev: "前へ", next: "次へ", books: "冊", collections: "コレクション", original: "元のパス", rules: "漫画タイトルの整理", hint: "表示名だけを変更します。ファイル、フォルダ、お気に入りは維持されます。ルールは順番に適用されます。", pattern: "検索パターン（正規表現）", replacement: "置換文字列（空欄可）", add: "ルールを追加", remove: "削除", preview: "プレビュー", save: "保存", reset: "既定値に戻す", saved: "保存済み", samples: "名前のプレビュー", noSamples: "ライブラリをスキャンして名前を確認", directory: "フォルダ" },
  ko: { title: "만화 폴더", all: "모든 라이브러리", root: "만화", search: "만화 검색", go: "검색", loading: "불러오는 중…", empty: "이 폴더에 만화가 없습니다", prev: "이전", next: "다음", books: "권", collections: "컬렉션", original: "원본 경로", rules: "만화 제목 정리", hint: "표시 이름만 변경합니다. 파일, 폴더와 즐겨찾기는 유지됩니다. 규칙은 순서대로 적용됩니다.", pattern: "검색 규칙 (정규 표현식)", replacement: "바꿀 내용 (빈 값 허용)", add: "규칙 추가", remove: "제거", preview: "미리보기", save: "저장", reset: "기본값 복원", saved: "저장됨", samples: "이름 미리보기", noSamples: "라이브러리를 스캔하여 이름 확인", directory: "폴더" },
};
function labels(locale: string) { return copy[locale as keyof typeof copy] || copy.en; }
function message(error: unknown) { return error instanceof Error ? error.message : String(error); }

export function ComicBrowser({ libraries, locale, onOpen, renderCover, refreshKey }: { libraries: Library[]; locale: string; onOpen: (series: Series) => void; renderCover: (id: number, url?: string) => ReactNode; refreshKey: string }) {
  const t = labels(locale);
  const [location, setLocation] = useState({ libraryId: 0, path: "" });
  const [offset, setOffset] = useState(0);
  const [input, setInput] = useState(""); const [query, setQuery] = useState("");
  const [page, setPage] = useState<Page | null>(null); const [error, setError] = useState(""); const [loading, setLoading] = useState(false);
  const relevant = libraries.filter(l => l.assetType === "comic" || l.assetType === "mixed");
  useEffect(() => {
    const abort = new AbortController(); setLoading(true); setError(""); setPage(null);
    const params = new URLSearchParams({libraryId: String(location.libraryId), path: location.path, limit: "60", offset: String(offset), q: query});
    request<Page>(`/api/client/comic-browser?${params}`, {signal: abort.signal}).then(value => { if (!abort.signal.aborted) setPage(value); }).catch(e => { if (!abort.signal.aborted) setError(message(e)); }).finally(() => { if (!abort.signal.aborted) setLoading(false); });
    return () => abort.abort();
  }, [location, offset, query, refreshKey]);
  const navigate = (libraryId: number, path: string) => { setLocation({libraryId, path}); setOffset(0); setQuery(""); setInput(""); };
  if (!relevant.length) return null;
  return <section className="comicBrowser" aria-label={t.title}>
    <h2>{t.title}</h2>
    <div className="comicTools"><select aria-label={t.all} value={location.libraryId} onChange={e => navigate(Number(e.target.value), "")}><option value="0">{t.all}</option>{relevant.map(l => <option key={l.id} value={l.id}>{displayText(l.name)}</option>)}</select>
    <form onSubmit={e => {e.preventDefault();setQuery(input);setOffset(0);}}><input aria-label={t.search} placeholder={t.search} value={input} onChange={e => setInput(e.target.value)} /><button>{t.go}</button></form></div>
    <nav className="comicBreadcrumbs" aria-label={t.directory}><button onClick={() => navigate(location.libraryId, "")}>{displayText(libraries.find(l => l.id === location.libraryId)?.name) || t.root}</button>{location.path.split("/").filter(Boolean).map((part, index, all) => <button key={index} onClick={() => navigate(location.libraryId, all.slice(0,index+1).join("/"))}>/ {displayText(part)}</button>)}</nav>
    {error && <p role="alert">{error}</p>}{loading && <p role="status">{t.loading}</p>}
    {!loading && page?.items.length === 0 && <p>{t.empty}</p>}
    <div className="comicNodes">{page?.items.map(item => <article key={`${item.kind}-${item.libraryId}-${item.id || item.path}`}>
      <button className="comicNode" onClick={() => item.kind === "folder" ? navigate(item.libraryId,item.path) : onOpen({...item, directoryPath: item.directoryPath || item.path} as Series)}>
        <span className="comicNodeCover">{item.coverBookId ? renderCover(item.coverBookId, item.thumbnailUrl) : <span aria-hidden="true">▧</span>}</span>
        <span className="comicNodeText"><strong>{collectionDisplayTitle({ ...item, directoryPath: item.directoryPath || item.path }, libraries.find(l => l.id === item.libraryId))}</strong><small>{collectionCountScope(item.kind, locale)} · {item.kind === "folder" ? `${item.collectionCount || 0} ${t.collections} · ` : ""}{item.bookCount} {t.books}</small><small>{displayText(libraries.find(l => l.id === item.libraryId)?.name)}{item.kind === "folder" ? ` · ${t.directory} ›` : ""}</small></span>
      </button>
      <details><summary>{t.original}</summary><code>{originalCollectionPath(item.path, libraries.find(l => l.id === item.libraryId))}</code></details>
    </article>)}</div>
    {page && <nav className="comicPagination" aria-label={t.title}><button disabled={loading || offset === 0} onClick={() => setOffset(Math.max(0,offset-60))}>{t.prev}</button><span>{page.total ? offset+1 : 0}–{offset+page.items.length} / {page.total}</span><button disabled={loading || !page.hasMore} onClick={() => setOffset(offset+60)}>{t.next}</button></nav>}
  </section>;
}

export function ComicTitleRules({ libraries, locale, onSaved }: { libraries: Library[]; locale: string; onSaved: () => void }) {
  const t = labels(locale); const relevant = libraries.filter(l => l.assetType === "comic" || l.assetType === "mixed");
  const [selected, setSelected] = useState(0); const id = relevant.some(l=>l.id===selected) ? selected : relevant[0]?.id;
  const [rules, setRules] = useState<Rule[]>([]); const [result, setResult] = useState<Rules | null>(null); const [error, setError] = useState(""); const [status, setStatus] = useState(""); const [busy, setBusy] = useState(false);
  useEffect(() => {
    if (!id) return; const abort = new AbortController(); setResult(null);setRules([]);setError("");setStatus("");setBusy(true);
    request<Rules>(`/api/comic-title-rules?libraryId=${id}`, {signal:abort.signal}).then(value=>{if(!abort.signal.aborted){setRules(value.rules);setResult(value);}}).catch(e=>{if(!abort.signal.aborted)setError(message(e));}).finally(()=>{if(!abort.signal.aborted)setBusy(false);});
    return ()=>abort.abort();
  },[id]);
  async function action(method: "POST" | "PUT", reset = false) {
    setBusy(true);setError("");setStatus("");
    try {const value = await request<Rules>(`/api/comic-title-rules?libraryId=${id}`,{method,body:JSON.stringify(reset?{reset:true}:{rules})});setResult(value);if(reset)setRules(value.rules);if(method==="PUT"){setStatus(t.saved);onSaved();}}
    catch(e){setError(message(e));}finally{setBusy(false);}
  }
  if (!id) return null;
  return <section className="panel wide comicRules"><h2>{t.rules}</h2><p>{t.hint}</p><select aria-label={t.rules} disabled={busy} value={id} onChange={e=>setSelected(Number(e.target.value))}>{relevant.map(l=><option key={l.id} value={l.id}>{displayText(l.name)}</option>)}</select>
  {rules.map((rule,index)=><div className="comicRule" key={index}><label>{t.pattern}<input value={rule.pattern} disabled={busy} onChange={e=>setRules(old=>old.map((r,i)=>i===index?{...r,pattern:e.target.value}:r))}/></label><label>{t.replacement}<input value={rule.replacement} disabled={busy} onChange={e=>setRules(old=>old.map((r,i)=>i===index?{...r,replacement:e.target.value}:r))}/></label><button disabled={busy} onClick={()=>setRules(old=>old.filter((_,i)=>i!==index))}>{t.remove}</button></div>)}
  <div className="comicTools"><button disabled={busy || !result || rules.length>=16} onClick={()=>setRules(old=>[...old,{pattern:"",replacement:""}])}>{t.add}</button><button disabled={busy || !result} onClick={()=>void action("POST")}>{t.preview}</button><button disabled={busy || !result} onClick={()=>void action("PUT")}>{t.save}</button><button disabled={busy || !result} onClick={()=>void action("PUT",true)}>{t.reset}</button></div>
  {error && <p role="alert">{error}</p>}<p role="status">{busy?t.loading:status}</p>
  <h3>{t.samples}</h3>{result?.samples.length ? <div className="comicSamples">{result.samples.map(sample=><div key={sample.id}><span>{sample.original}</span><span aria-hidden="true"> → </span><strong>{sample.displayTitle}</strong></div>)}</div>:<p>{t.noSamples}</p>}
  </section>;
}
