import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import ts from "typescript";
const source = await readFile(new URL("./book-health.ts", import.meta.url), "utf8");
const compiled = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ES2020 } }).outputText;
const {bookHealthCopy,bookFileIssueLabel,bookFileIssueReason} = await import(`data:text/javascript;base64,${Buffer.from(compiled).toString("base64")}`);
test("known book file problems have labels, reasons and recovery guidance in every UI language",()=>{
 for (const locale of ["zh","zht","en","ja","ko"]) {
  for (const code of ["empty_file","zero_filled_file","invalid_book_container","invalid_epub"]) {
   assert.notEqual(bookFileIssueLabel(code,locale),code);assert.ok(bookFileIssueReason(code,locale));
  }
  assert.ok(bookHealthCopy(locale).resolved);assert.ok(bookHealthCopy(locale).hint);
 }
 assert.equal(bookFileIssueLabel("permission_denied","zh"),"permission_denied");
 assert.equal(bookFileIssueReason("permission_denied","zh"),"");
});
