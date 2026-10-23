export type ReaderProgressSession = {
  readonly generation: number;
  readonly bookId: number;
  readonly profileId: string;
  readonly revision: number;
  ready: boolean;
  dirty: boolean;
};

type MutableReaderProgressSession = { -readonly [Key in keyof ReaderProgressSession]: ReaderProgressSession[Key] };

// A session represents one explicit book-open operation, even if the same book
// is reopened. Loading and layout events never make a session dirty.
export class ReaderProgressSessions {
  current: ReaderProgressSession | null = null;
  private generation = 0;
  private saveQueues = new Map<string, Promise<void>>();

  start(bookId: number, profileId: string): ReaderProgressSession {
    const session = { generation: ++this.generation, bookId, profileId, revision: 0, ready: false, dirty: false };
    this.current = session;
    return session;
  }

  invalidate(): void {
    this.generation++;
    this.current = null;
  }

  isCurrent(session: ReaderProgressSession | null): session is ReaderProgressSession {
    return session !== null && session === this.current;
  }

  markReady(session: ReaderProgressSession): boolean {
    if (!this.isCurrent(session)) return false;
    session.ready = true;
    return true;
  }

  markUserChange(): boolean {
    const session = this.current;
    if (!session?.ready) return false;
    (session as MutableReaderProgressSession).revision++;
    session.dirty = true;
    return true;
  }

  canSave(session: ReaderProgressSession | null, bookId: number, profileId: string): session is ReaderProgressSession {
    return this.isCurrent(session) && session.ready && session.dirty && session.bookId === bookId && session.profileId === profileId;
  }

  consume(session: ReaderProgressSession): boolean {
    if (!this.canSave(session, session.bookId, session.profileId)) return false;
    session.dirty = false;
    return true;
  }

  restoreDirtyAfterFailure(session: ReaderProgressSession, revision: number = session.revision): void {
    if (this.isCurrent(session) && session.ready && revision === session.revision) session.dirty = true;
  }

  enqueueSave(session: ReaderProgressSession, task: () => Promise<void>): Promise<void> {
    const key = this.saveKey(session);
    const previous = this.saveQueues.get(key) ?? Promise.resolve();
    const save = previous.then(async () => {
      if (this.isCurrent(session)) await task();
    });
    // Keep the tail fulfilled so a failed request cannot block a later user save.
    const tail = save.catch(() => undefined);
    this.saveQueues.set(key, tail);
    void tail.then(() => {
      if (this.saveQueues.get(key) === tail) this.saveQueues.delete(key);
    });
    return save;
  }

  async whenIdle(session: ReaderProgressSession): Promise<void> {
    const key = this.saveKey(session);
    let tail: Promise<void> | undefined;
    while ((tail = this.saveQueues.get(key))) await tail;
  }

  private saveKey(session: ReaderProgressSession): string {
    return `${session.profileId}:${session.bookId}`;
  }
}
