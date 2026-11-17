// Where the Extensions listing keeps what a reader chose: the view and the
// columns, in this browser. The other listings each keep their own the same
// way, under their own id.
import type { ListingStore } from '@prusa/connect-ui/primitives';

export const browserStore = (): ListingStore => ({
  read: (key) => localStorage.getItem(key),
  write: (key, value) => {
    localStorage.setItem(key, value);
  },
});
