import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const read = path => readFileSync(new URL(path, import.meta.url), 'utf8');
test('TMDB configuration is opt-in and real env files stay out of Git/build context', () => {
  const compose = read('../../docker-compose.yml');
  for (const name of ['FOLIOSPACE_VIDEO_METADATA_PROVIDER', 'FOLIOSPACE_TMDB_TOKEN']) {
    assert.ok(compose.includes(`${name}: \${${name}:-}`));
    assert.match(read('../../.env.example'), new RegExp(`^${name}=$`, 'm'));
  }
  assert.ok(compose.includes('FOLIOSPACE_TMDB_LANGUAGE: ${FOLIOSPACE_TMDB_LANGUAGE:-en-US}'));
  for (const path of ['../../.gitignore', '../../.dockerignore']) {
    assert.match(read(path), /^\.env$/m);
    assert.match(read(path), /^\.env\.\*$/m);
  }
});
test('web About and video library keep static local-logo TMDB attribution', () => {
  const app = read('./App.tsx');
  const about = app.slice(app.indexOf('{view === "about" && ('), app.indexOf('{view === "reader" && ('));
  for (const source of [about, read('./VideoLibrary.tsx')]) {
    assert.ok(source.includes('This product uses the TMDB API but is not endorsed or certified by TMDB.'));
    assert.ok(source.includes('src="/tmdb.svg"'));
    assert.ok(source.includes('alt="The Movie Database (TMDB)"'));
    assert.ok(!source.includes('FOLIOSPACE_TMDB_TOKEN'));
  }
});
