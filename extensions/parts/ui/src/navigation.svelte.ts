// Where in the catalogue the person is, kept in the address bar.
//
// A module's page has one route of its own, `/m/<namespace>/parts`, and the
// app's router serves no path under it. So the folder, the open part and the
// search ride in the query: `?folder=4`, `?part=12`, `?q=hook&tag=garage`. A
// person can then send a link to one part, and the back button walks back
// through the folders they opened.
//
// The router reads the address bar on `popstate`. This store writes the
// history itself and then tells the router, by the same event, so the router
// and the page agree on the address. A click the router took on a link of its
// own (the rail entry back to the bare page) is read after it is taken, so
// that one lands here too.
//
// A class, because it is state the whole page shares (svelte-5.md). The
// browser comes in through the constructor, which is what lets a test drive
// it without one.
import { searchOf, type Spot, spotOf, TOP } from './navigation-query';

export { type Spot, TOP } from './navigation-query';

/** What the store needs of the browser. `window` is one. */
export interface Address {
  readonly location: { readonly pathname: string; readonly search: string };
  readonly history: {
    pushState(data: unknown, unused: string, url: string): void;
  };
  addEventListener(type: string, listener: () => void): void;
  removeEventListener(type: string, listener: () => void): void;
  dispatchEvent(event: Event): boolean;
}

export class Navigation {
  readonly #address: Address | null;
  #spot = $state.raw<Spot>(TOP);
  /** The page's own path, read when the page mounts. */
  #path = '';
  /** Who is told when the place moves. */
  #movers: ((spot: Spot) => void)[] = [];

  constructor(address: Address | null) {
    this.#address = address;
  }

  /** Where the person is. Read-only by design: `go` is how it changes. */
  get spot(): Spot {
    return this.#spot;
  }

  /** True while a search or a tag narrows the listing across every folder. */
  get searching(): boolean {
    return this.#spot.q !== '' || this.#spot.tag !== '';
  }

  /**
   * Starts reading the address bar for the page that just mounted, and
   * answers the call that stops it. The page calls it from `onMount` and
   * returns the answer, so nothing outlives the page (svelte-testing.md).
   */
  attach(): () => void {
    const address = this.#address;
    if (address === null) return () => undefined;

    this.#path = address.location.pathname;
    this.#read();

    const read = (): void => {
      this.#read();
    };
    // The router takes a click on one of its own links in its own listener,
    // which ran before this one. The address it wrote is read once it has.
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

  /**
   * Tells listener about every move, made here or by the browser, and
   * answers the call that stops telling it.
   */
  onMove(listener: (spot: Spot) => void): () => void {
    this.#movers = [...this.#movers, listener];

    return () => {
      this.#movers = this.#movers.filter((held) => held !== listener);
    };
  }

  /** The address of one place, for a link a person can open in a new tab. */
  href(spot: Spot): string {
    return `${this.#path}${searchOf(spot)}`;
  }

  /** Moves to one place, and writes it into the history. */
  go(spot: Spot): void {
    this.#spot = spot;
    this.#leave(this.href(spot));
    this.#told();
  }

  /** Opens a part, keeping the folder it is in. */
  openPart(part: number, folder: number): void {
    this.go({ ...TOP, folder, part });
  }

  /** Opens a folder, closing any part and any search. */
  openFolder(folder: number): void {
    this.go({ ...TOP, folder });
  }

  /** Narrows the listing to a search or a tag, across every folder. */
  search(q: string, tag: string): void {
    this.go({ ...TOP, folder: this.#spot.folder, q, tag });
  }

  /** Leaves the catalogue for another screen of the app, such as a printer. */
  leave(href: string): void {
    this.#leave(href);
  }

  #read(): void {
    const address = this.#address;
    if (address === null) return;
    // A path that is not the page's own is another screen. The store keeps
    // the place it had, and the page that mounts next reads its own.
    if (address.location.pathname !== this.#path) return;

    const next = spotOf(address.location.search);
    if (searchOf(next) === searchOf(this.#spot)) return;
    this.#spot = next;
    this.#told();
  }

  #told(): void {
    for (const listener of this.#movers) listener(this.#spot);
  }

  #leave(href: string): void {
    const address = this.#address;
    if (address === null) return;
    const current = `${address.location.pathname}${address.location.search}`;
    if (current === href) return;

    address.history.pushState(null, '', href);
    // The router reads the address bar on this event, and on no other, so it
    // is told the same way the back button tells it.
    address.dispatchEvent(new PopStateEvent('popstate'));
  }
}
