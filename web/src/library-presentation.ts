import type { Book, GameCatalogTaskStatus, Library, ScanJob, Series, ThumbnailWorkerStatus } from "./api";

// Presentation only: raw titles, paths and private metadata remain API identities.
export function displayText(value?: string | null): string {
  return (value || "").trim().replace(/\s+/g, " ");
}

export function distinctMetadata(parts: (string | undefined)[], excluded: (string | undefined)[] = []): string {
  const seen = new Set(excluded.map(value => displayText(value).toLocaleLowerCase()).filter(Boolean));
  return parts.map(displayText).filter(value => {
    const key = value.toLocaleLowerCase();
    if (!key || seen.has(key)) return false;
    seen.add(key);
    return true;
  }).join(" · ");
}

export function collectionDisplayTitle(item: Pick<Series, "title" | "directoryPath"> & { displayTitle?: string }, library?: Library): string {
  const title = displayText(item.displayTitle) || displayText(item.title);
  if (title && title !== ".") return title;
  return displayText(library?.name) || displayText(item.directoryPath).split("/").filter(part => part && part !== ".").pop() || displayText(library?.rootPath).split("/").filter(Boolean).pop() || "—";
}

export function originalCollectionPath(path: string | undefined, library?: Library): string {
  return !path || path === "." ? library?.rootPath || displayText(library?.name) || "—" : path;
}

export function bookDisplayCreator(book: Pick<Book, "creator"> & { displayCreator?: string }): string {
  return displayText(book.displayCreator ?? book.creator);
}

export function bookCollectionTitle(book: Pick<Book, "collectionTitle"> & { collectionDisplayTitle?: string }): string {
  const value = displayText(book.collectionDisplayTitle) || displayText(book.collectionTitle);
  return value === "." ? "" : value;
}

const labels = {
  zh: { direct: "本目录", subtree: "含子目录", loaded: "已加载", discs: "张光盘", scan: "资源库扫描", videoScan: "视频库扫描", target: "扫描范围", recentJobs: "最近的扫描记录；每项对应一次扫描请求。", processed: "已检查", files: "已处理文件", matched: "新增封面", unchanged: "未新增封面", classified: "分类已调整", unmodified: "分类未变", checksummed: "已补齐校验", skipped: "跳过", failed: "失败", coverHint: "已检查包括已有封面和没有找到新封面的条目；未新增不代表失败。", analyzeHint: "已检查是分析过的条目，分类未变也属于正常处理。", cacheHint: "任务数来自任务记录；磁盘缓存是上次刷新时检查到的文件数。", cacheUnchecked: "磁盘缓存尚未检查，点击刷新查看。", cacheUnavailable: "未检查", jobCount: "缩略图任务记录" },
  zht: { direct: "本目錄", subtree: "含子目錄", loaded: "已載入", discs: "張光碟", scan: "資源庫掃描", videoScan: "影片庫掃描", target: "掃描範圍", recentJobs: "最近的掃描記錄；每項對應一次掃描請求。", processed: "已檢查", files: "已處理檔案", matched: "新增封面", unchanged: "未新增封面", classified: "分類已調整", unmodified: "分類未變", checksummed: "已補齊校驗", skipped: "略過", failed: "失敗", coverHint: "已檢查包括已有封面和沒有找到新封面的項目；未新增不代表失敗。", analyzeHint: "已檢查是分析過的項目，分類未變也屬於正常處理。", cacheHint: "任務數來自任務記錄；磁碟快取是上次重新整理時檢查到的檔案數。", cacheUnchecked: "磁碟快取尚未檢查，點擊重新整理查看。", cacheUnavailable: "未檢查", jobCount: "縮圖任務記錄" },
  en: { direct: "This folder", subtree: "Including subfolders", loaded: "Loaded", discs: "discs", scan: "Library scan", videoScan: "Video library scan", target: "Scan scope", recentJobs: "Recent scan records; each entry represents one scan request.", processed: "Checked", files: "Files processed", matched: "New covers", unchanged: "No new cover", classified: "Classification changed", unmodified: "Classification unchanged", checksummed: "Checksums added", skipped: "Skipped", failed: "Failed", coverHint: "Checked includes entries with existing covers and no new match; no new cover does not mean failure.", analyzeHint: "Checked counts analyzed entries, including those whose classification stayed the same.", cacheHint: "Task counts come from job records; disk cache counts are from the last explicit refresh.", cacheUnchecked: "Disk cache has not been checked. Refresh to inspect it.", cacheUnavailable: "Not checked", jobCount: "Thumbnail job records" },
  ja: { direct: "このフォルダ", subtree: "サブフォルダを含む", loaded: "読み込み済み", discs: "枚のディスク", scan: "ライブラリスキャン", videoScan: "動画ライブラリスキャン", target: "スキャン範囲", recentJobs: "最近のスキャン履歴。各項目は1回のスキャン要求です。", processed: "確認済み", files: "処理済みファイル", matched: "新しい表紙", unchanged: "新しい表紙なし", classified: "分類変更", unmodified: "分類変更なし", checksummed: "チェックサム追加", skipped: "スキップ", failed: "失敗", coverHint: "確認済みには既存の表紙や新しい一致がない項目も含まれます。新しい表紙なしは失敗ではありません。", analyzeHint: "確認済みは分類に変更がない項目も含みます。", cacheHint: "タスク数はジョブ履歴、ディスクキャッシュは前回の更新時に確認したファイル数です。", cacheUnchecked: "ディスクキャッシュは未確認です。更新して確認してください。", cacheUnavailable: "未確認", jobCount: "サムネイルジョブ履歴" },
  ko: { direct: "현재 폴더", subtree: "하위 폴더 포함", loaded: "불러옴", discs: "개 디스크", scan: "라이브러리 스캔", videoScan: "동영상 라이브러리 스캔", target: "스캔 범위", recentJobs: "최근 스캔 기록이며 각 항목은 한 번의 스캔 요청입니다.", processed: "확인됨", files: "처리한 파일", matched: "새 표지", unchanged: "새 표지 없음", classified: "분류 변경", unmodified: "분류 변경 없음", checksummed: "체크섬 추가", skipped: "건너뜀", failed: "실패", coverHint: "확인됨에는 기존 표지와 새 일치 항목이 없는 경우도 포함됩니다. 새 표지가 없어도 실패는 아닙니다.", analyzeHint: "확인됨에는 분류가 바뀌지 않은 항목도 포함됩니다.", cacheHint: "작업 수는 작업 기록이며 디스크 캐시는 마지막 새로고침에서 확인한 파일 수입니다.", cacheUnchecked: "디스크 캐시를 아직 확인하지 않았습니다. 새로고침하여 확인하세요.", cacheUnavailable: "미확인", jobCount: "썸네일 작업 기록" },
};
export function presentationCopy(locale: string) { return labels[locale as keyof typeof labels] || labels.en; }
export function collectionCountScope(kind: "folder" | "collection", locale: string): string {
  const copy = presentationCopy(locale);
  return kind === "folder" ? copy.subtree : copy.direct;
}
export function loadedDiscCount(count: number, locale: string): string {
  const copy = presentationCopy(locale);
  return `${copy.loaded} ${count} ${copy.discs}`;
}

const states: Record<string, [string, string, string, string, string]> = {
  "needs-curation": ["待整理", "待整理", "Needs curation", "要整理", "정리 필요"],
  "missing-cover": ["缺少封面", "缺少封面", "Missing cover", "表紙なし", "표지 없음"],
  "unmatched-metadata": ["元数据未匹配", "中繼資料未比對", "Metadata not matched", "メタデータ未照合", "메타데이터 미일치"],
  "launch-profile-missing": ["缺少启动配置", "缺少啟動設定", "Missing launch profile", "起動プロファイルなし", "실행 프로필 없음"],
  "identity-missing": ["缺少文件身份校验", "缺少檔案身分校驗", "Missing file identity", "ファイル識別情報なし", "파일 식별 정보 없음"],
  "policy-pack-missing": ["缺少兼容性策略", "缺少相容性策略", "Missing compatibility policy", "互換性ポリシーなし", "호환성 정책 없음"],
  "dependency-missing": ["缺少启动文件", "缺少啟動檔案", "Missing launch files", "起動ファイルなし", "실행 파일 없음"],
  "manifest-checksum-unavailable": ["启动文件待校验", "啟動檔案待校驗", "Launch checksums pending", "起動ファイルの検証待ち", "실행 파일 체크섬 대기"],
  playable: ["可运行", "可執行", "Playable", "プレイ可能", "플레이 가능"],
  broken: ["无法运行", "無法執行", "Not working", "動作不可", "실행 불가"],
  untested: ["尚未测试", "尚未測試", "Untested", "未検証", "테스트 전"],
  unknown: ["未知", "未知", "Unknown", "不明", "알 수 없음"],
  game: ["游戏", "遊戲", "Game", "ゲーム", "게임"],
  dependency: ["依赖文件", "相依檔案", "Dependency", "依存ファイル", "종속 파일"],
  ready: ["已就绪", "已就緒", "Ready", "準備完了", "준비됨"],
  missing: ["缺失", "缺失", "Missing", "なし", "없음"],
  matched: ["已匹配", "已比對", "Matched", "照合済み", "일치함"],
  unmatched: ["未匹配", "未比對", "Not matched", "未照合", "미일치"],
  manual: ["人工设置", "手動設定", "Manual", "手動", "수동"],
  local: ["本地", "本機", "Local", "ローカル", "로컬"],
  pending: ["待处理", "待處理", "Pending", "保留中", "대기 중"],
  disabled: ["已禁用", "已停用", "Disabled", "無効", "사용 안 함"],
};
export function gameStateLabel(value: string | undefined, locale: string): string {
  const key = displayText(value);
  const index = ["zh", "zht", "en", "ja", "ko"].indexOf(locale);
  return states[key]?.[index < 0 ? 2 : index] || key;
}
export function gameTaskActionLabel(action: string | undefined, locale: string): string {
  const actions: Record<string, [string, string, string, string, string]> = {
    analyze: ["目录分类分析", "目錄分類分析", "Catalog classification", "カタログ分類", "목록 분류"],
    checksums: ["补齐文件校验", "補齊檔案校驗", "Add file checksums", "チェックサム補完", "파일 체크섬 보완"],
    "compatibility-rebuild": ["补齐兼容数据", "補齊相容資料", "Complete compatibility data", "互換性データ補完", "호환성 데이터 보완"],
    "covers-local": ["匹配本地封面", "比對本機封面", "Match local covers", "ローカル表紙照合", "로컬 표지 찾기"],
    "covers-libretro": ["匹配 Libretro 封面", "比對 Libretro 封面", "Match Libretro covers", "Libretro 表紙照合", "Libretro 표지 찾기"],
  };
  const index = ["zh", "zht", "en", "ja", "ko"].indexOf(locale);
  return actions[action || ""]?.[index < 0 ? 2 : index] || displayText(action);
}
export function gameTaskSummary(task: GameCatalogTaskStatus, locale: string): { text: string; hint: string } {
  const copy = presentationCopy(locale);
  const covers = task.action?.startsWith("covers-");
  const analysis = task.action === "analyze";
  const processed = `${analysis || covers ? copy.processed : copy.files} ${task.processed}${task.total > 0 ? ` / ${task.total}` : ""}`;
  const changes = `${covers ? copy.matched : analysis ? copy.classified : copy.checksummed} ${task.matched}`;
  const unchanged = analysis || covers ? `${covers ? copy.unchanged : copy.unmodified} ${Math.max(0, task.processed - task.matched - task.failed)}` : `${copy.skipped} ${task.skipped || 0}`;
  return { text: [processed, changes, unchanged, `${copy.failed} ${task.failed}`].join(" · "), hint: covers ? copy.coverHint : analysis ? copy.analyzeHint : "" };
}
export function hasThumbnailCacheDetails(status: ThumbnailWorkerStatus | null): boolean {
  return Boolean(status?.cache?.algorithmVersion?.trim());
}
export function mergeThumbnailStatus(previous: ThumbnailWorkerStatus | null, next: ThumbnailWorkerStatus): ThumbnailWorkerStatus {
  return !hasThumbnailCacheDetails(next) && hasThumbnailCacheDetails(previous) ? { ...next, cache: previous!.cache } : next;
}
export function scanJobDescription(job: ScanJob, library: Library | undefined, locale: string): string {
  const copy = presentationCopy(locale);
  return distinctMetadata([library?.assetType === "video" ? copy.videoScan : copy.scan, library?.name]);
}

export function gameIssueDescription(code: string | undefined, fallback: string | undefined, locale: string): string {
  const descriptions: Record<string, [string, string, string, string, string]> = {
    dependency: ["BIOS、设备、父 ROM 或音轨依赖，不单独出现在客户端。", "BIOS、裝置、父 ROM 或音軌相依檔案，不單獨顯示於用戶端。", "BIOS, device, parent ROM or track dependency, hidden as a standalone client entry.", "BIOS、デバイス、親 ROM またはトラックの依存ファイルです。", "BIOS, 장치, 상위 ROM 또는 트랙 종속 파일입니다."],
    "identity-missing": ["压缩包缺少稳定的 SHA-1 校验，请重新扫描该文件。", "壓縮檔缺少穩定的 SHA-1 校驗，請重新掃描該檔案。", "The archive needs a stable SHA-1 identity. Rescan the file.", "SHA-1 識別情報が必要です。再スキャンしてください。", "안정적인 SHA-1 식별 정보가 필요합니다. 다시 스캔하세요."],
    "policy-pack-missing": ["尚未安装 FBNeo 或 MAME 兼容性策略。", "尚未安裝 FBNeo 或 MAME 相容性策略。", "No FBNeo or MAME compatibility policy is installed.", "FBNeo または MAME の互換性ポリシーがありません。", "FBNeo 또는 MAME 호환성 정책이 없습니다."],
    "launch-profile-missing": ["尚无已审核的启动配置；可检查兼容性策略及父 ROM、BIOS 依赖。", "尚無已審核的啟動設定；可檢查相容性策略及父 ROM、BIOS 相依檔案。", "No audited launch profile is available. Check compatibility policies and parent ROM or BIOS dependencies.", "検証済み起動プロファイルがありません。ポリシーと依存ファイルを確認してください。", "검증된 실행 프로필이 없습니다. 호환성 정책과 종속 파일을 확인하세요."],
    "dependency-missing": ["启动清单没有已索引的文件，请重新扫描该游戏。", "啟動清單沒有已索引的檔案，請重新掃描該遊戲。", "The launch manifest has no indexed files. Rescan this game.", "起動ファイルが索引にありません。再スキャンしてください。", "색인된 실행 파일이 없습니다. 게임을 다시 스캔하세요."],
    "manifest-checksum-unavailable": ["部分启动文件仍需校验，完成后客户端才能使用。", "部分啟動檔案仍需校驗，完成後用戶端才能使用。", "Some launch files still need checksums before clients can use them.", "一部の起動ファイルのチェックサムが必要です。", "일부 실행 파일에 체크섬이 필요합니다."],
  };
  const index = ["zh", "zht", "en", "ja", "ko"].indexOf(locale);
  return descriptions[code || ""]?.[index < 0 ? 2 : index] || displayText(fallback) || gameStateLabel(code || "ready", locale);
}

export function ownershipPreviewCopy(locale: string) {
  const copy = {
    zh: { preview: "预览归属与排除", loading: "正在检查…", hint: "扫描时更具体的资源库优先拥有文件。此预览仅检查现有记录。", excluded: "扫描自动排除的资源库路径", none: "无", conflicts: "现有归属冲突", examples: "冲突示例（最多 12 项）", current: "当前", expected: "归属应为" },
    zht: { preview: "預覽歸屬與排除", loading: "正在檢查…", hint: "掃描時更具體的資源庫優先擁有檔案。此預覽僅檢查現有記錄。", excluded: "掃描自動排除的資源庫路徑", none: "無", conflicts: "現有歸屬衝突", examples: "衝突範例（最多 12 項）", current: "目前", expected: "應歸屬於" },
    en: { preview: "Preview ownership and exclusions", loading: "Checking…", hint: "Scans assign files to the most specific library root. This preview only inspects existing records.", excluded: "Library paths excluded from scans", none: "None", conflicts: "Existing ownership conflicts", examples: "Conflict examples (up to 12)", current: "Current", expected: "Expected" },
    ja: { preview: "所属と除外を確認", loading: "確認中…", hint: "スキャンは最も具体的なライブラリを優先します。既存の記録を確認するだけです。", excluded: "スキャンから除外するパス", none: "なし", conflicts: "既存の所属競合", examples: "競合の例（最大12件）", current: "現在", expected: "所属先" },
    ko: { preview: "소속 및 제외 미리보기", loading: "확인 중…", hint: "스캔은 가장 구체적인 라이브러리 경로를 우선합니다. 기존 기록을 확인만 합니다.", excluded: "스캔에서 제외되는 경로", none: "없음", conflicts: "기존 소속 충돌", examples: "충돌 예시 (최대 12개)", current: "현재", expected: "예상 소속" },
  };
  return copy[locale as keyof typeof copy] || copy.en;
}
