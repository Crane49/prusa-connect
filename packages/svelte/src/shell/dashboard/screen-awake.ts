// Keeps the screen on while a wall view is up, where the browser offers a
// wake lock. The browser lets the lock go when the tab hides, and the system
// may let it go at any moment, for example on low battery. So the lock is
// asked for again whenever the page is visible and holds none. A browser that
// refuses leaves the screen to its own power settings.

/** A held lock, as the browser's `WakeLockSentinel` is one. */
export interface HeldLock extends EventTarget {
  readonly released: boolean;
  release(): Promise<void>;
}

/** What asks for a lock, as the browser's `navigator.wakeLock` does. */
export interface ScreenLock {
  request(type: 'screen'): Promise<HeldLock>;
}

/** The page whose visibility decides whether a lock is asked for. */
export interface ShownPage extends EventTarget {
  readonly visibilityState: DocumentVisibilityState;
}

/** Asks for the lock and asks again whenever it is lost. Returns the stop. */
export const keepScreenAwake = (
  wakeLock: ScreenLock | undefined,
  page: ShownPage,
): (() => void) => {
  if (wakeLock === undefined) return () => undefined;

  let lock: HeldLock | undefined;
  // The mount and a change of visibility can both ask before the first
  // answer comes, and two answers would leave one lock nobody lets go.
  let asking = false;
  let stopped = false;
  // Read through a call after an await. TypeScript keeps the value it saw
  // before the await and calls a plain check of the flag always false.
  const isStopped = (): boolean => stopped;

  const take = async (): Promise<void> => {
    if (stopped || asking || page.visibilityState !== 'visible') return;
    if (lock !== undefined && !lock.released) return;
    asking = true;
    try {
      const taken = await wakeLock.request('screen');
      // The request answers later. A wall view gone by then lets the lock go
      // at once, or the screen would stay on after it closed.
      if (isStopped()) {
        void taken.release().catch(() => undefined);

        return;
      }
      taken.addEventListener(
        'release',
        () => {
          if (lock === taken) lock = undefined;
          void take();
        },
        { once: true },
      );
      lock = taken;
    } catch {
      lock = undefined;
    } finally {
      asking = false;
    }
  };

  const shown = (): void => {
    void take();
  };
  page.addEventListener('visibilitychange', shown);
  void take();

  return () => {
    stopped = true;
    page.removeEventListener('visibilitychange', shown);
    void lock?.release().catch(() => undefined);
    lock = undefined;
  };
};
