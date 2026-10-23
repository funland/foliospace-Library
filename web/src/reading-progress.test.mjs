import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';
const js = ts.transpileModule(readFileSync(new URL('./reading-progress.ts', import.meta.url), 'utf8'), { compilerOptions: { module: ts.ModuleKind.ES2020 } }).outputText;
const { readingProgress, isContinueReading } = await import(`data:text/javascript;base64,${Buffer.from(js).toString('base64')}`);
test('completed progress and manual completion leave continue reading; near completion stays', () => {
  assert.equal(isContinueReading({progressFraction: .996, privateStatus: 'reading'}), true);
  assert.equal(readingProgress({progressFraction: .996}), 99);
  assert.equal(isContinueReading({progressFraction: 1, privateStatus: 'reading'}), false);
  assert.equal(readingProgress({progressFraction: 1}), 100);
  assert.equal(isContinueReading({progressFraction: .2, privateStatus: 'finished'}), false);
  assert.equal(isContinueReading({progressFraction: .2, privateStatus: 'reading'}), true);
});

test('EPUB chapter completion is not book completion, including legacy saved progress', () => {
 const book = {format: 'epub', pageCount: 10, currentPage: 0, progressFraction: 1};
 assert.equal(isContinueReading(book), true);
 assert.equal(readingProgress(book), 10);
 assert.equal(isContinueReading({...book, currentPage: 9}), false);
 assert.equal(isContinueReading({...book, pageCount: 0}), true);
});
