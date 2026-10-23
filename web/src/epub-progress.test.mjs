import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';
const js = ts.transpileModule(readFileSync(new URL('./epub-progress.ts', import.meta.url), 'utf8'), { compilerOptions: { module: ts.ModuleKind.ES2020 } }).outputText;
const { resolveEpubProgress } = await import(`data:text/javascript;base64,${Buffer.from(js).toString('base64')}`);
const manifest = (paths) => ({spine: paths.map((href, index) => ({index, href}))});

test('native path identifies the EPUB spine even when the native page index has a different meaning', () => {
  const jinouque = manifest(['titlepage.xhtml', ...Array.from({length: 9}, (_, i) => `text/part${String(i).padStart(4, '0')}.html`)]);
  const restored = resolveEpubProgress({pageIndex: 0, locator: 'text/part0006.html', progressFraction: .08984375}, jinouque);
  assert.deepEqual(restored, {chapterIndex: 7, pagePosition: 0, progressFraction: .08984375, progressScope: 'book', locatorKind: 'path', matched: true});
  const narrow = manifest(['titlepage.xhtml', 'OEBPS/title.xhtml', 'OEBPS/contents.xhtml', 'OEBPS/recommend.xhtml', 'OEBPS/foreword.xhtml', 'OEBPS/introduction1.xhtml']);
  assert.equal(resolveEpubProgress({pageIndex: 2, locator: 'OEBPS/introduction1.xhtml', progressFraction: .10162601626016261}, narrow).chapterIndex, 5);
});

test('only a complete integer locator restores chapter-relative web pagination', () => {
  const spine = manifest(['text/title.xhtml', 'text/12chapter.xhtml', 'text/chapter.xhtml']);
  assert.deepEqual(resolveEpubProgress({pageIndex: 2, locator: '13', progressFraction: .5}, spine), {chapterIndex: 2, pagePosition: 13, progressFraction: .5, progressScope: 'chapter', locatorKind: 'page', matched: true});
  assert.equal(resolveEpubProgress({pageIndex: 2, locator: '12chapter.xhtml', progressFraction: .5}, spine).chapterIndex, 1);
  for (const locator of ['12oops', '-1', '1.5', '', ' 12 ', '9007199254740992']) {
    const restored = resolveEpubProgress({pageIndex: 2, locator, progressFraction: .5}, spine);
    assert.equal(restored.pagePosition, 0);
    assert.equal(restored.progressScope, 'book');
  }
});

test('path restoration supports package prefixes, encoded fragments and unambiguous suffixes', () => {
  const spine = manifest(['OEBPS/text/chapter one.xhtml', 'OEBPS/text/chapter2.xhtml']);
  const restored = resolveEpubProgress({pageIndex: 1, locator: './text/chapter%20one.xhtml#note%201', progressFraction: .2}, spine);
  assert.equal(restored.chapterIndex, 0);
  assert.equal(restored.fragment, 'note 1');
  assert.equal(restored.progressFraction, .2);
  const ambiguous = manifest(['vol1/chapter.xhtml', 'vol2/chapter.xhtml']);
  assert.equal(resolveEpubProgress({pageIndex: 1, locator: 'chapter.xhtml', progressFraction: .2}, ambiguous).matched, false);
  assert.equal(resolveEpubProgress({pageIndex: 1, locator: 'chapter.xhtml', progressFraction: .2}, ambiguous).chapterIndex, 1);
});

test('unknown locators preserve the saved fraction and safely clamp the fallback chapter', () => {
  const restored = resolveEpubProgress({pageIndex: 999, locator: 'missing.xhtml#anchor', progressFraction: .1}, manifest(['a.xhtml', 'b.xhtml']));
  assert.deepEqual(restored, {chapterIndex: 1, pagePosition: 0, progressFraction: .1, progressScope: 'book', locatorKind: 'unknown', matched: false});
});
