/**
 * Starts at most `size` jobs at once, in the order they asked. Taking a slot
 * hands over the job's start and returns the one way to give the slot back:
 * called before the job started it leaves the queue, called after it frees the
 * slot for the next in line. A second call does nothing.
 */
export const slots = (size: number): ((start: () => void) => () => void) => {
  let busy = 0;
  const queue: (() => void)[] = [];

  const next = (): void => {
    while (busy < size) {
      const start = queue.shift();
      if (start === undefined) return;
      busy += 1;
      start();
    }
  };

  return (start) => {
    let state: 'queued' | 'running' | 'done' = 'queued';
    const run = (): void => {
      state = 'running';
      start();
    };
    queue.push(run);
    next();

    return () => {
      const was = state;
      state = 'done';
      if (was === 'queued') queue.splice(queue.indexOf(run), 1);
      if (was === 'running') {
        busy -= 1;
        next();
      }
    };
  };
};

/**
 * One queue for every thumbnail on the page. The page is served over
 * HTTP/1.1, where a browser keeps six connections to the server, and a
 * picture of a file on a printer waits on a printer that runs one command at
 * a time and can be busy for fifteen seconds. A screen of those would hold all
 * six and leave the page's own reads queued behind them in the browser. Two
 * leaves four for the page. A picture from the server's own disk answers in
 * milliseconds, so it loses nothing by sharing the queue, and no caller has to
 * say which kind it draws. Page-wide on purpose: the connection limit it
 * guards is page-wide too. Only a mounted thumbnail in a browser takes a slot,
 * so a server render never touches it.
 */
export const takeThumbnailSlot = slots(2);
