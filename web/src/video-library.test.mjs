import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';
const library = readFileSync(new URL('./VideoLibrary.tsx', import.meta.url), 'utf8');
const editor = readFileSync(new URL('./VideoMetadataEditor.tsx', import.meta.url), 'utf8');
const css = readFileSync(new URL('./video-library.css', import.meta.url), 'utf8');
const app = readFileSync(new URL('./App.tsx', import.meta.url), 'utf8');
const poster = readFileSync(new URL('./VideoPoster.tsx', import.meta.url), 'utf8');
const posterCache = readFileSync(new URL('./video-poster-cache.ts', import.meta.url), 'utf8');
const styles = readFileSync(new URL('./styles.css', import.meta.url), 'utf8');
const homeSource = readFileSync(new URL('./home-library.ts', import.meta.url), 'utf8');
const homeJS = ts.transpileModule(homeSource, { compilerOptions: { module: ts.ModuleKind.ES2020 } }).outputText;
const { homeLibrarySections, isReadingOrGameCollection } = await import(`data:text/javascript;base64,${Buffer.from(homeJS).toString('base64')}`);
test('home separates video-only, reading, game, mixed and empty libraries', () => {
  assert.deepEqual(homeLibrarySections([{ assetType: 'video' }], false), { video: true, legacy: false });
  for (const assetType of ['book', 'comic', 'game', 'mixed']) {
    assert.deepEqual(homeLibrarySections([{ assetType }], false), { video: false, legacy: true });
    assert.deepEqual(homeLibrarySections([{ assetType }], true), { video: true, legacy: true });
  }
  assert.deepEqual(homeLibrarySections([], false), { video: false, legacy: false });
  assert.deepEqual(homeLibrarySections([{ assetType: 'video' }, { assetType: 'comic' }], true), { video: true, legacy: true });
});
test('stale video-directory records never enter reading collections, even in mixed libraries', () => {
  const libs = [{ id: 1, assetType: 'video' }, { id: 2, assetType: 'mixed' }];
  assert.equal(isReadingOrGameCollection({ libraryId: 1, primaryType: 'comic' }, libs), false);
  assert.equal(isReadingOrGameCollection({ libraryId: 2, primaryType: 'video' }, libs), false);
  for (const primaryType of ['book', 'comic', 'game']) {
    assert.equal(isReadingOrGameCollection({ libraryId: 2, primaryType }, libs), true);
  }
});
test('home replaces BOTH legacy collections and volume wall with shared video works', () => {
  const home = app.slice(app.indexOf('{!selectedSeries && homeSections.video && ('), app.indexOf('{view === "favorites" && ('));
  assert.match(home, /<VideoLibrary presentation="home"/);
  assert.match(home, /setSelectedVideo\(video\)/);
  assert.match(home, /openVideoCatalog\(\)/);
  const legacy = home.slice(home.indexOf('{homeSections.legacy && <>'), home.indexOf('</>}'));
  assert.match(legacy, /collectionPanel panel/);
  assert.match(legacy, /coverWall panel collectionContent/);
  assert.match(app, /isReadingOrGameCollection\(item, libraries\)/);
  assert.match(library, /作品集 · 电影与剧集/);
  assert.match(library, /text\("Cover wall", "封面墙"\)/);
  assert.match(library, /\/api\/client\/video-works/);
  assert.match(css, /\.videoHomeLibrary\{grid-column:1\/-1/);
});
test('home and catalog use shared posters, never video frame thumbnails', () => {
  const tile = app.slice(app.indexOf('function VideoTile('), app.indexOf('function compareVideosByTitle'));
  assert.match(tile, /<VideoPoster videoId=\{video.id\}/);
  assert.doesNotMatch(tile, /thumbnailUrl|thumbnailFailed|Now Showing/);
  assert.match(library, /<VideoPoster videoId=\{w.videoId\} revision=\{revision\}/);
  assert.match(library, /<VideoPoster videoId=\{id\} revision=\{revision\}/);
  assert.match(posterCache, /\/artwork\/poster/);
  assert.doesNotMatch(poster, /\/thumbnail|ffmpeg\(/);
  assert.match(poster, /key=\{src\}/);
  assert.doesNotMatch(poster, /Date.now|&view=/);
  assert.match(posterCache, /poster\?r=\$\{revision\}/);
  assert.match(styles, /\.videoCover \{[\s\S]*?aspect-ratio: 2 \/ 3/);
});
test('poster URLs survive remount but explicit edits and refresh revisions invalidate them', async () => {
  const js = ts.transpileModule(posterCache, { compilerOptions: { module: ts.ModuleKind.ES2020 } }).outputText;
  const { videoPosterSource, invalidateVideoPoster, invalidateAllVideoPosters } = await import(`data:text/javascript;base64,${Buffer.from(js).toString('base64')}`);
  const original = videoPosterSource(1);
  assert.equal(videoPosterSource(1), original);
  invalidateVideoPoster(2);
  assert.equal(videoPosterSource(1), original);
  invalidateVideoPoster(1);
  assert.notEqual(videoPosterSource(1), original);
  assert.notEqual(videoPosterSource(1, 1), videoPosterSource(1));
  const beforeReset = [videoPosterSource(1), videoPosterSource(2)];
  invalidateAllVideoPosters();
  assert.notEqual(videoPosterSource(1), beforeReset[0]);
  assert.notEqual(videoPosterSource(2), beforeReset[1]);
  assert.match(editor, /await fn\(\); invalidateVideoPoster\(videoId\); onChange\(\)/);
});
test('home recent video shelf uses existing newest-first data and hides stale books in video-only libraries', () => {
  assert.match(app, /setVideoShelf\(arrayOrEmpty\(home.videoShelf\)\)/);
  assert.match(app, /title=\{t.videoShelf\}\s+subtitle=\{t.recentSubtitle\}/);
  assert.match(app, /libraries.some\(library => library.assetType !== "video"\)/);
  assert.match(app, /\{showRecentBooks &&/);
});
test('video catalog uses bounded paging and cancels obsolete reads', () => {
  assert.match(library, /limit: "24"/); assert.match(library, /AbortController/); assert.match(library, /abort\.abort\(\)/); assert.match(library, /page\?\.hasMore/);
});
test('unknown files and original file routes remain accessible', () => {
  assert.match(library, /All files \/ unmatched/); assert.match(library, /\/api\/client\/videos/); assert.doesNotMatch(library, /hls\/index|transcode\/start/);
});
test('manual fields, unlink, candidate confirmation, and image controls are explicit', () => {
  for (const route of ['metadata/unlink','metadata/select-match','artwork/select','metadata/refresh']) assert.ok(editor.includes(route));
  assert.match(editor, /meta\?\.generation/); assert.match(editor, /lockedFields/); assert.match(editor, /window\.confirm/); assert.match(editor, /method: "DELETE"/);
});
test('poster layout preserves two readable columns on narrow screens', () => {
  assert.match(css, /max-width:520px/); assert.match(css, /repeat\(2,minmax\(0,1fr\)\)/); assert.match(css, /aspect-ratio:2\/3/);
});
