const copy = {
 zh: { empty: "空文件", zero: "全零文件", invalid: "文件损坏或格式异常", epub: "EPUB 结构异常", resolved: "已恢复 · 历史记录", hint: "异常书籍已从最近添加隐藏，源文件和已有记录保留。补回完整文件并重新扫描后可恢复显示；历史错误不会删除。", emptyReason: "文件大小为 0 字节，没有可读取内容。", zeroReason: "文件虽然有大小，但内容全部为零字节，无法读取。", invalidReason: "文件格式或压缩包目录无法读取，可能已损坏或截断。", epubReason: "无法读取 EPUB 的书籍信息或阅读顺序。" },
 zht: { empty: "空檔案", zero: "全零檔案", invalid: "檔案損壞或格式異常", epub: "EPUB 結構異常", resolved: "已恢復 · 歷史記錄", hint: "異常書籍已從最近新增隱藏，原始檔案和既有記錄保留。補回完整檔案並重新掃描後可恢復顯示；歷史錯誤不會刪除。", emptyReason: "檔案大小為 0 位元組，沒有可讀取內容。", zeroReason: "檔案內容全部為零位元組，無法讀取。", invalidReason: "無法讀取檔案格式或壓縮檔目錄，可能已損壞或截斷。", epubReason: "無法讀取 EPUB 的書籍資訊或閱讀順序。" },
 en: { empty: "Empty file", zero: "Zero-filled file", invalid: "Damaged or invalid file", epub: "Invalid EPUB structure", resolved: "Recovered · history", hint: "Books with file errors are hidden from Recently Added. Source files and records are retained. Restore a complete file and rescan to show it again; historical errors remain.", emptyReason: "The file is 0 bytes and contains no readable content.", zeroReason: "The file contains only zero bytes and cannot be read.", invalidReason: "The file format or ZIP directory cannot be read; the file may be damaged or truncated.", epubReason: "The EPUB package or reading order cannot be read." },
 ja: { empty: "空のファイル", zero: "全ゼロのファイル", invalid: "破損または形式エラー", epub: "EPUB 構造エラー", resolved: "復旧済み · 履歴", hint: "異常のある本は最近追加した本から非表示になります。元ファイルと記録は保持されます。完全なファイルを復元して再スキャンすると再表示され、エラー履歴も残ります。", emptyReason: "ファイルは 0 バイトで、読める内容がありません。", zeroReason: "ファイルの内容がすべてゼロバイトです。", invalidReason: "形式または ZIP ディレクトリを読めません。破損や切り詰めの可能性があります。", epubReason: "EPUB の書籍情報または読む順序を読み取れません。" },
 ko: { empty: "빈 파일", zero: "0으로 채워진 파일", invalid: "손상 또는 형식 오류", epub: "EPUB 구조 오류", resolved: "복구됨 · 기록", hint: "오류가 있는 책은 최근 추가에서 숨겨집니다. 원본 파일과 기록은 유지됩니다. 완전한 파일을 복원하고 다시 스캔하면 표시되며 오류 기록은 남습니다.", emptyReason: "파일 크기가 0바이트로 읽을 내용이 없습니다.", zeroReason: "파일 내용이 모두 0바이트로 채워져 있습니다.", invalidReason: "파일 형식이나 ZIP 디렉터리를 읽을 수 없습니다. 손상되거나 잘렸을 수 있습니다.", epubReason: "EPUB 책 정보나 읽기 순서를 읽을 수 없습니다." },
};
export function bookHealthCopy(locale: string) { return copy[locale as keyof typeof copy] || copy.en; }
export function bookFileIssueLabel(code: string, locale: string) {
 const c = bookHealthCopy(locale);
 return ({ empty_file: c.empty, zero_filled_file: c.zero, invalid_book_container: c.invalid, invalid_epub: c.epub } as Record<string,string>)[code] || code;
}
export function bookFileIssueReason(code: string, locale: string) {
 const c = bookHealthCopy(locale);
 return ({ empty_file: c.emptyReason, zero_filled_file: c.zeroReason, invalid_book_container: c.invalidReason, invalid_epub: c.epubReason } as Record<string,string>)[code] || "";
}
