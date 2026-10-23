import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';
const js = ts.transpileModule(readFileSync(new URL('./reader-progress-session.ts', import.meta.url), 'utf8'), { compilerOptions: { module: ts.ModuleKind.ES2020 } }).outputText;
const { ReaderProgressSessions } = await import(`data:text/javascript;base64,${Buffer.from(js).toString('base64')}`);

test('opening, failed restoration and layout measurements never authorize a zero save', () => {
  const sessions = new ReaderProgressSessions();
  const session = sessions.start(42, '1');
  assert.equal(sessions.markUserChange(), false);
  assert.equal(sessions.canSave(session, 42, '1'), false);
  assert.equal(sessions.consume(session), false);
  sessions.markReady(session);
  assert.equal(sessions.canSave(session, 42, '1'), false);
  sessions.markUserChange();
  assert.equal(sessions.canSave(session, 42, '1'), true);
  assert.equal(sessions.consume(session), true);
  assert.equal(sessions.canSave(session, 42, '1'), false);
});

test('stale loads, callbacks and profile mismatches cannot save into the active reader', () => {
  const sessions = new ReaderProgressSessions();
  const first = sessions.start(1, '1');
  sessions.markReady(first);
  sessions.markUserChange();
  const second = sessions.start(2, '1');
  assert.equal(sessions.markReady(first), false);
  assert.equal(sessions.consume(first), false);
  sessions.restoreDirtyAfterFailure(first);
  sessions.markReady(second);
  assert.equal(second.dirty, false);
  sessions.markUserChange();
  assert.equal(sessions.canSave(second, 1, '1'), false);
  assert.equal(sessions.canSave(second, 2, '2'), false);
  sessions.invalidate();
  assert.equal(sessions.consume(second), false);
  const reopened = sessions.start(2, '1');
  assert.notEqual(reopened.generation, second.generation);
  assert.equal(sessions.isCurrent(second), false);
});

test('explicit backwards navigation remains saveable and failures do not dirty another session', () => {
  const sessions = new ReaderProgressSessions();
  const session = sessions.start(42, '1');
  sessions.markReady(session);
  for (const page of [66, 8, 0]) {
    sessions.markUserChange();
    assert.equal(sessions.consume(session), true, `explicit page ${page} remains valid`);
  }
  sessions.restoreDirtyAfterFailure(session);
  assert.equal(session.dirty, true);
  const next = sessions.start(42, '2');
  sessions.markReady(next);
  sessions.restoreDirtyAfterFailure(session);
  assert.equal(next.dirty, false);
});

function deferred() {
  let resolve;
  const promise = new Promise((done) => { resolve = done; });
  return { promise, resolve };
}

test('saves for one profile/book cannot finish in reverse navigation order', async () => {
  const sessions = new ReaderProgressSessions();
  const session = sessions.start(42, '1');
  const firstStarted = deferred();
  const releaseFirst = deferred();
  const writes = [];
  const first = sessions.enqueueSave(session, async () => {
    firstStarted.resolve();
    await releaseFirst.promise;
    writes.push(66);
  });
  await firstStarted.promise;
  const second = sessions.enqueueSave(session, async () => { writes.push(8); });
  let idle = false;
  const idlePromise = sessions.whenIdle(session).then(() => { idle = true; });
  await Promise.resolve();
  assert.deepEqual(writes, []);
  assert.equal(idle, false);
  releaseFirst.resolve();
  await Promise.all([first, second, idlePromise]);
  assert.deepEqual(writes, [66, 8]);
  assert.equal(idle, true);
});

test('reopening the same book waits for its in-flight save and drops unstarted stale work', async () => {
  const sessions = new ReaderProgressSessions();
  const old = sessions.start(42, '1');
  const started = deferred();
  const release = deferred();
  const writes = [];
  const first = sessions.enqueueSave(old, async () => {
    started.resolve();
    await release.promise;
    writes.push('old in flight');
  });
  await started.promise;
  const stale = sessions.enqueueSave(old, async () => { writes.push('stale queued'); });
  const reopened = sessions.start(42, '1');
  const current = sessions.enqueueSave(reopened, async () => { writes.push('current'); });
  release.resolve();
  await Promise.all([first, stale, current, sessions.whenIdle(reopened)]);
  assert.deepEqual(writes, ['old in flight', 'current']);
});

test('a failed save does not stall later writes or consume a newer user change', async () => {
  const sessions = new ReaderProgressSessions();
  const session = sessions.start(42, '1');
  sessions.markReady(session);
  sessions.markUserChange();
  sessions.consume(session);
  const first = sessions.enqueueSave(session, async () => { throw new Error('network'); });
  sessions.markUserChange();
  const writes = [];
  const second = sessions.enqueueSave(session, async () => { writes.push(8); });
  await assert.rejects(first, /network/);
  await second;
  assert.deepEqual(writes, [8]);
  assert.equal(session.dirty, true);
});

test('an in-flight save for a different book does not block the current book', async () => {
  const sessions = new ReaderProgressSessions();
  const firstSession = sessions.start(1, '1');
  const started = deferred();
  const release = deferred();
  const first = sessions.enqueueSave(firstSession, async () => {
    started.resolve();
    await release.promise;
  });
  await started.promise;
  const nextSession = sessions.start(2, '1');
  let wrote = false;
  await sessions.enqueueSave(nextSession, async () => { wrote = true; });
  assert.equal(wrote, true);
  release.resolve();
  await first;
});

test('failure of an older user revision cannot make a newer queued save dirty again', () => {
  const sessions = new ReaderProgressSessions();
  const session = sessions.start(42, '1');
  sessions.markReady(session);
  sessions.markUserChange();
  const firstRevision = session.revision;
  sessions.consume(session);
  sessions.markUserChange();
  const secondRevision = session.revision;
  sessions.consume(session);
  sessions.restoreDirtyAfterFailure(session, firstRevision);
  assert.equal(session.dirty, false);
  sessions.restoreDirtyAfterFailure(session, secondRevision);
  assert.equal(session.dirty, true);
});
