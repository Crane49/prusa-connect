// The page's state, kept in the address bar (page-state.ts says how it is
// spelt). Switching a tab is a step the back button walks back through; a
// filter or a fold rewrites the step it is on, so the back button does not
// replay every letter typed into a search.
//
// The app's router reads the address on `popstate` only, so a write here is
// followed by that event, the way extensions/parts/ui/src/navigation.svelte.ts
// tells it. A class, because the page and its views share it (svelte-5.md).
// The browser comes in through the constructor, which is what lets a test
// drive it without one.
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
  /** Moves each time the browser, not the page, changes the state. */
  #visits = $state(0);
  #path = '';

  constructor(address: Address | null) {
    this.#address = address;
  }

  get state(): PageState {
    return this.#state;
  }

  /**
   * Counts the times the back or forward button changed the state. A view
   * that holds its own copy of the filters is drawn again when it moves.
   */
  get visits(): number {
    return this.#visits;
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
      this.#visits += 1;
    };

    address.addEventListener('popstate', read);

    return () => {
      address.removeEventListener('popstate', read);
    };
  }

  /** Opens a tab, as a step of its own in the history. */
  openTab(tab: PageState['tab']): void {
    this.#write({ ...this.#state, tab }, 'push');
  }

  /** Records a change of filters or folds on the step the page is on. */
  update(next: Partial<PageState>): void {
    this.#write({ ...this.#state, ...next }, 'replace');
  }

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
