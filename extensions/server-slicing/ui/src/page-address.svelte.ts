// The page's state, kept in the address bar (page-state.ts says how it is
// spelt). Switching a tab and opening a release or a job are steps the back
// button walks back through. A filter, a search or a sort rewrites the step
// it is on, so the back button does not replay every letter typed.
//
// The app's router reads the address on `popstate` only, so a write here is
// followed by that event, the way extensions/parts/ui/src/navigation.svelte.ts
// tells it. A class, because the page and its views share it (svelte-5.md).
// The browser comes in through the constructor, which is what lets a test and
// a story drive it without one.
import { DEFAULT_STATE, type PageState, searchOf, stateOf } from './page-state';

/** What the store needs of the browser. `window` is one. */
export interface Address {
  readonly location: { readonly pathname: string; readonly search: string };
  readonly history: {
    pushState(data: unknown, unused: string, url: string): void;
    replaceState(data: unknown, unused: string, url: string): void;
  };
  addEventListener(type: string, listener: () => void): void;
  removeEventListener(type: string, listener: () => void): void;
  dispatchEvent(event: Event): boolean;
}

export class PageAddress {
  readonly #address: Address | null;
  #state = $state.raw<PageState>(DEFAULT_STATE);
  /** The page's own path, read when the page mounts. Empty without a browser. */
  #path = '';

  constructor(address: Address | null, initial: PageState = DEFAULT_STATE) {
    this.#address = address;
    this.#state = initial;
  }

  get state(): PageState {
    return this.#state;
  }

  /** Reads the address for the page that just mounted; the answer stops it. */
  attach(): () => void {
    const address = this.#address;
    if (address === null) return () => undefined;

    this.#path = address.location.pathname;
    this.#state = stateOf(address.location.search);

    const read = (): void => {
      // Another screen's address: the page that mounts next reads its own.
      if (address.location.pathname !== this.#path) return;
      const next = stateOf(address.location.search);
      if (searchOf(next) === searchOf(this.#state)) return;
      this.#state = next;
    };
    // The router takes a click on one of its own links in its own listener.
    // The address it wrote is read once it has.
    const clicked = (): void => {
      queueMicrotask(read);
    };

    address.addEventListener('popstate', read);
    address.addEventListener('click', clicked);

    return () => {
      address.removeEventListener('popstate', read);
      address.removeEventListener('click', clicked);
    };
  }

  /** The address of a state, for a real link a person can open in a new tab. */
  href(next: Partial<PageState>): string {
    return `${this.#path}${searchOf({ ...this.#state, ...next })}` || '?';
  }

  /** Moves to a tab, a release or a job, as a step of its own. */
  go(next: Partial<PageState>): void {
    this.#write({ ...this.#state, ...next }, 'push');
  }

  /** Records a change of filters or order on the step the page is on. */
  update(next: Partial<PageState>): void {
    this.#write({ ...this.#state, ...next }, 'replace');
  }

  /**
   * Takes a plain click on a link to a state of this page, so the page moves
   * without the router remounting it. Put on the page's root as `onclick`. A
   * click with a modifier, or a middle click, is left to the browser.
   */
  take = (event: MouseEvent): void => {
    if (event.defaultPrevented || event.button !== 0) return;
    if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) {
      return;
    }
    if (!(event.target instanceof Element)) return;
    const anchor = event.target.closest('a[href]');
    const href = anchor?.getAttribute('href') ?? '';
    if (!href.startsWith(`${this.#path}?`)) return;
    if (anchor?.getAttribute('target')) return;

    event.preventDefault();
    this.#write(stateOf(href.slice(this.#path.length)), 'push');
  };

  #write(next: PageState, how: 'push' | 'replace'): void {
    this.#state = next;
    const address = this.#address;
    if (address === null || this.#path === '') return;

    const href = `${this.#path}${searchOf(next)}`;
    if (`${address.location.pathname}${address.location.search}` === href) {
      return;
    }

    if (how === 'push') address.history.pushState(null, '', href);
    else address.history.replaceState(null, '', href);
    address.dispatchEvent(new PopStateEvent('popstate'));
  }
}
