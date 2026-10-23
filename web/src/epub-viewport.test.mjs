import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import ts from 'typescript';

test('EPUB observes the actual frame, coalesces resize and cancels on chapter/unmount', async () => {
  const source = readFileSync(new URL('./epub-viewport.ts', import.meta.url), 'utf8');
  const js = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ES2020 } }).outputText;
  const { observeEpubViewport } = await import(`data:text/javascript;base64,${Buffer.from(js).toString('base64')}`);
  const originals = [globalThis.ResizeObserver, globalThis.requestAnimationFrame, globalThis.cancelAnimationFrame];
  let resize, observed, disconnected = false, id = 0, layouts = 0;
  const queue = new Map();
  globalThis.ResizeObserver = class {
    constructor(callback) { resize = callback; }
    observe(frame) { observed = frame; }
    disconnect() { disconnected = true; }
  };
  globalThis.requestAnimationFrame = callback => { queue.set(++id, callback); return id; };
  globalThis.cancelAnimationFrame = handle => queue.delete(handle);
  try {
    const frame = {};
    const cleanup = observeEpubViewport(frame, () => layouts++);
    assert.equal(observed, frame);
    resize(); resize(); resize();
    assert.equal(queue.size, 1);
    for (const callback of queue.values()) callback();
    queue.clear();
    assert.equal(layouts, 1);
    resize(); cleanup();
    assert.equal(disconnected, true);
    assert.equal(queue.size, 0);
  } finally {
    [globalThis.ResizeObserver, globalThis.requestAnimationFrame, globalThis.cancelAnimationFrame] = originals;
  }
  const app = readFileSync(new URL('./App.tsx', import.meta.url), 'utf8');
  assert.match(app, /observeEpubViewport\(frame, applyEpubLayout\)/);
  assert.match(app, /column-count: auto !important/); // Retain the historical nested-column fix.
});
