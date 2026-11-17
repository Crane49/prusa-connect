// The wall view's wake lock, over a fake browser lock and a fake page.
import { describe, expect, it } from 'vitest';

import {
  type HeldLock,
  keepScreenAwake,
  type ScreenLock,
  type ShownPage,
} from './screen-awake';

class FakeHeld extends EventTarget implements HeldLock {
  released = false;

  release(): Promise<void> {
    if (!this.released) {
      this.released = true;
      this.dispatchEvent(new Event('release'));
    }

    return Promise.resolve();
  }
}

/** A browser lock whose requests answer when the test says so. */
class FakeLock implements ScreenLock {
  readonly given: FakeHeld[] = [];
  readonly #waiting: (() => void)[] = [];

  request(): Promise<HeldLock> {
    return new Promise((resolve) => {
      this.#waiting.push(() => {
        const held = new FakeHeld();
        this.given.push(held);
        resolve(held);
      });
    });
  }

  get asked(): number {
    return this.given.length + this.#waiting.length;
  }

  /** Answers every request that waits, and lets the answers land. */
  async answer(): Promise<void> {
    for (const run of this.#waiting.splice(0)) run();
    await Promise.resolve();
    await Promise.resolve();
  }

  get held(): number {
    return this.given.filter((lock) => !lock.released).length;
  }
}

class FakePage extends EventTarget implements ShownPage {
  visibilityState: DocumentVisibilityState = 'visible';

  show(state: DocumentVisibilityState): void {
    this.visibilityState = state;
    this.dispatchEvent(new Event('visibilitychange'));
  }
}

describe('keepScreenAwake', () => {
  it('asks once while a request is on its way, whatever asks again', async () => {
    const lock = new FakeLock();
    const page = new FakePage();
    const stop = keepScreenAwake(lock, page);

    page.show('visible');
    page.show('visible');
    await lock.answer();

    expect(lock.asked).toBe(1);
    expect(lock.held).toBe(1);
    stop();
  });

  it('takes the lock again when the system lets it go while the page is visible', async () => {
    const lock = new FakeLock();
    const page = new FakePage();
    const stop = keepScreenAwake(lock, page);
    await lock.answer();

    await lock.given[0]?.release();
    await lock.answer();

    expect(lock.asked).toBe(2);
    expect(lock.held).toBe(1);
    stop();
  });

  it('waits for the page to come back before it asks again', async () => {
    const lock = new FakeLock();
    const page = new FakePage();
    const stop = keepScreenAwake(lock, page);
    await lock.answer();

    page.visibilityState = 'hidden';
    await lock.given[0]?.release();
    expect(lock.asked).toBe(1);

    page.show('visible');
    await lock.answer();
    expect(lock.held).toBe(1);
    stop();
  });

  it('lets go of the lock when stopped, and of one that answers after the stop', async () => {
    const lock = new FakeLock();
    const page = new FakePage();
    const stop = keepScreenAwake(lock, page);
    await lock.answer();
    stop();
    expect(lock.held).toBe(0);

    const late = keepScreenAwake(lock, page);
    late();
    await lock.answer();
    expect(lock.held).toBe(0);
    // A stopped helper does not ask again after the release.
    expect(lock.asked).toBe(2);
  });

  it('does nothing in a browser without a wake lock', () => {
    const stop = keepScreenAwake(undefined, new FakePage());

    expect(stop).not.toThrow();
  });
});
