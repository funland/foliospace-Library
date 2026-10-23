import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';
const js = ts.transpileModule(readFileSync(new URL('./reading-progress.ts', import.meta.url), 'utf8'), { compilerOptions: { module: ts.ModuleKind.ES2020 } }).outputText;
const { readingProgress, isContinueReading, shouldRestoreWebtoonPosition } = await import(`data:text/javascript;base64,${Buffer.from(js).toString('base64')}`);
test('completed progress and manual completion leave continue reading; near completion stays', () => {
  assert.equal(isContinueReading({progressFraction: .996, privateStatus: 'reading'}), true);
  assert.equal(readingProgress({progressFraction: .996}), 99);
  assert.equal(isContinueReading({progressFraction: 1, privateStatus: 'reading'}), false);
  assert.equal(readingProgress({progressFraction: 1}), 100);
  assert.equal(isContinueReading({progressFraction: .2, privateStatus: 'finished'}), false);
  assert.equal(isContinueReading({progressFraction: .2, privateStatus: 'reading'}), true);
});

test('integer EPUB locators distinguish chapter completion from book completion', () => {
 const book = {format: 'epub', pageCount: 10, currentPage: 0, progressFraction: 1, progressLocator: '5'};
 assert.equal(isContinueReading(book), true);
 assert.equal(readingProgress(book), 10);
 assert.equal(isContinueReading({...book, currentPage: 9}), false);
 assert.equal(isContinueReading({...book, pageCount: 0}), true);
});

test('native EPUB path locators retain whole-book progress rather than dividing it twice', () => {
  assert.equal(readingProgress({format: 'epub', pageCount: 62, currentPage: 0, progressFraction: .08984375, progressLocator: 'text/part0006.html'}), 9);
  assert.equal(readingProgress({format: 'epub', pageCount: 42, currentPage: 2, progressFraction: .10162601626016261, progressLocator: 'OEBPS/introduction1.xhtml'}), 10);
  assert.equal(isContinueReading({format: 'epub', pageCount: 42, currentPage: 2, progressFraction: 1, progressLocator: 'OEBPS/introduction1.xhtml'}), false);
  assert.equal(readingProgress({format: 'epub', pageCount: 42, currentPage: 2, progressFraction: .10162601626016261}), 10);
  assert.equal(readingProgress({format: 'epub', pageCount: 62, currentPage: 5, progressFraction: 1/23, progressLocator: '1'}), 8);
  assert.equal(readingProgress({format: 'epub', pageCount: 62, currentPage: 0, progressFraction: .09, progressLocator: '01.xhtml'}), 9);
});

test('opening paged comics ignores saved webtoon positions and older positions never displace newer general progress', () => {
  const general = {locator: '067.jpg', updatedAt: '2026-10-22 06:36:16'};
  const oldWebtoon = {updatedAt: '2026-09-20T12:38:28Z'};
  assert.equal(shouldRestoreWebtoonPosition(general, oldWebtoon, 'single'), false);
  assert.equal(shouldRestoreWebtoonPosition(general, oldWebtoon, 'double'), false);
  assert.equal(shouldRestoreWebtoonPosition(general, oldWebtoon, 'webtoon'), false);
  assert.equal(shouldRestoreWebtoonPosition(general, {updatedAt: '2026-10-22T14:36:16+08:00'}, 'webtoon'), true);
  assert.equal(shouldRestoreWebtoonPosition(general, {updatedAt: '2026-10-22T14:36:15+08:00'}, 'webtoon'), false);
  assert.equal(shouldRestoreWebtoonPosition(general, {updatedAt: '2026-10-22T14:36:17+08:00'}, 'single'), false);
});

test('missing or ambiguous timestamps cannot promote unrelated webtoon history', () => {
  assert.equal(shouldRestoreWebtoonPosition({locator: '067.jpg'}, {}, 'webtoon'), false);
  assert.equal(shouldRestoreWebtoonPosition({locator: '067.jpg', updatedAt: '2026-10-22T06:36:16'}, {updatedAt: '2026-10-22T06:36:17'}, 'webtoon'), false);
  assert.equal(shouldRestoreWebtoonPosition({locator: 'webtoon:0.2'}, {}, 'webtoon'), true);
  assert.equal(shouldRestoreWebtoonPosition({locator: 'webtoon:0.2'}, null, 'webtoon'), false);
});
