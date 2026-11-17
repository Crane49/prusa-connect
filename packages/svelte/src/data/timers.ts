// The clock a store polls on, as an injected object.
//
// A store that polls needs a timer, and a test needs to move that timer
// without waiting in real time. So the timer comes in through the store's
// options, the same way the client does, and `systemTimers` is the default an
// application gets.

/** What a polling store needs of a clock. */
export interface Timers {
  /** Runs `task` after `ms` milliseconds and returns a handle for `clear`. */
  set(task: () => void, ms: number): number;
  /** Cancels a task that has not run. */
  clear(handle: number): void;
}

/**
 * The browser's own timers. `setTimeout` answers a `Timeout` object under Node
 * and a number in a browser, so the handle is narrowed to a number here and is
 * only ever passed back to `clearTimeout`.
 */
export const systemTimers: Timers = {
  set: (task, ms) => setTimeout(task, ms) as unknown as number,
  clear: (handle) => {
    clearTimeout(handle);
  },
};

/**
 * Runs `task` once the browser has drawn the frame it is in the middle of.
 *
 * A dialog that fetches from the handler that opened it holds the frame: the
 * request is built, the SDK runs, and the panel is drawn after all of it. The
 * frame callback runs before that drawing and the timeout after it, so the
 * pair is what puts work behind the paint rather than in front of it. A page
 * with no frame clock, such as a server render, runs the task at once.
 */
export const afterPaint = (task: () => void): void => {
  if (typeof requestAnimationFrame !== 'function') {
    task();

    return;
  }

  requestAnimationFrame(() => {
    setTimeout(task, 0);
  });
};
