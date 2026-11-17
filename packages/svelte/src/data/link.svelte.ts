// The Fractal links this server declares, for the Fractal page.
import { type DataFault, faultOf } from './fault';
import {
  approveNode,
  type LinkClient,
  type LinkInstance,
  type LinkNode,
  NODE_PENDING,
  readLink,
  readLinks,
} from './services/link';
import { systemTimers, type Timers } from './timers';

/** How the store is built beyond its client. */
export interface LinkOptions {
  /** The wait between refreshes, in milliseconds. */
  readonly refreshMs?: number;
  readonly timers?: Timers;
}

const DEFAULT_REFRESH_MS = 5000;

/**
 * A shared store, so a class (svelte-5.md). The client comes in through the
 * constructor, which lets a test pass a fake.
 *
 * A node's `alive` and `latency_ms` move without an event, so the page reads
 * the links again on a tick rather than waiting for one. The tick runs only
 * while the page asks for it, and `stop()` ends it.
 */
export class Link {
  readonly #client: LinkClient;
  readonly #timers: Timers;
  readonly #refreshMs: number;

  #items = $state.raw<readonly LinkInstance[]>([]);
  #name = $state('');
  #loading = $state(false);
  #loaded = $state(false);
  #error = $state<DataFault | undefined>(undefined);
  #approving = $state.raw<readonly string[]>([]);
  #flight: Promise<void> | undefined;
  #handle: number | undefined;

  constructor(client: LinkClient, options: LinkOptions = {}) {
    this.#client = client;
    this.#timers = options.timers ?? systemTimers;
    this.#refreshMs = options.refreshMs ?? DEFAULT_REFRESH_MS;
  }

  /** Read-only by design: the collection is the server's (svelte-5.md). */
  get items(): readonly LinkInstance[] {
    return this.#items;
  }

  get loading(): boolean {
    return this.#loading;
  }

  /** True once a read has answered, which tells a server with no link from an unread one. */
  get loaded(): boolean {
    return this.#loaded;
  }

  get error(): DataFault | undefined {
    return this.#error;
  }

  /** The link being looked at. The first one when nothing was selected. */
  readonly current: LinkInstance | undefined = $derived(
    this.#name === ''
      ? this.#items[0]
      : this.#items.find((row) => row.name === this.#name),
  );

  /** True when this server declares a link at all. */
  readonly declared: boolean = $derived(this.#items.length > 0);

  /** True when the link is switched on. */
  readonly enabled: boolean = $derived(this.current?.enabled ?? false);

  /** True when the link is switched on and running. */
  readonly running: boolean = $derived(this.current?.running ?? false);

  /** This server's role in the tree, as the instance reports it. */
  readonly mode: string | undefined = $derived(this.current?.mode);

  /** How a node joins: it inherits the parent's permissions, or it is granted them. */
  readonly enrolment: string | undefined = $derived(this.current?.enrolment);

  /** This server's key, which the parent admits it by. */
  readonly publicKey: string | undefined = $derived(this.current?.public_key);

  /** The other servers on the link. */
  readonly nodes: readonly LinkNode[] = $derived(this.current?.nodes ?? []);

  /** The nodes that have joined and are talking. */
  readonly alive: readonly LinkNode[] = $derived(
    this.nodes.filter((node) => node.alive),
  );

  /** The nodes waiting for somebody here to admit them. */
  readonly pending: readonly LinkNode[] = $derived(
    this.nodes.filter((node) => node.admission === NODE_PENDING),
  );

  /** True while an approval for this node is in flight. */
  approving(nodeId: string): boolean {
    return this.#approving.includes(nodeId);
  }

  /** Looks at one link by name. An empty name goes back to the first one. */
  select(name: string): void {
    this.#name = name;
  }

  /** A second call while one read is in flight is given that read. */
  load(): Promise<void> {
    if (this.#flight) return this.#flight;

    const flight = this.#read().finally(() => {
      if (this.#flight === flight) this.#flight = undefined;
    });
    this.#flight = flight;

    return flight;
  }

  /**
   * Reads the links every `refreshMs`. The returned function stops the tick,
   * and the page calls it when it closes (svelte-testing.md).
   */
  listen(): () => void {
    this.#schedule();

    return () => {
      this.stop();
    };
  }

  /** Stops the refresh tick. */
  stop(): void {
    if (this.#handle === undefined) return;

    this.#timers.clear(this.#handle);
    this.#handle = undefined;
  }

  /**
   * Admits one waiting node. The answer is the whole instance, so the row is
   * replaced from it rather than by reading the list again.
   */
  async approve(nodeId: string): Promise<void> {
    const name = this.current?.name;
    if (name === undefined || this.approving(nodeId)) return;

    this.#approving = [...this.#approving, nodeId];
    this.#error = undefined;
    try {
      this.#replace(await approveNode(this.#client, name, nodeId));
    } catch (thrown) {
      this.#error = faultOf(thrown);
    } finally {
      this.#approving = this.#approving.filter((held) => held !== nodeId);
    }
  }

  /** Reads one link again, for a page looking at a single instance. */
  async refresh(name: string): Promise<void> {
    try {
      this.#replace(await readLink(this.#client, name));
      this.#error = undefined;
    } catch (thrown) {
      this.#error = faultOf(thrown);
    }
  }

  #replace(instance: LinkInstance): void {
    const at = this.#items.findIndex((row) => row.name === instance.name);
    if (at < 0) {
      this.#items = [...this.#items, instance];

      return;
    }

    const next = [...this.#items];
    next[at] = instance;
    this.#items = next;
  }

  // The tick goes through load() rather than straight to the read, so a
  // refresh that fires while the page's own read is in flight joins it instead
  // of sending a second one (svelte-data.md).
  #schedule(): void {
    this.stop();
    this.#handle = this.#timers.set(() => {
      this.#handle = undefined;
      void this.load().finally(() => {
        this.#schedule();
      });
    }, this.#refreshMs);
  }

  async #read(): Promise<void> {
    this.#loading = true;
    try {
      const page = await readLinks(this.#client);
      this.#items = page.items ?? [];
      this.#loaded = true;
      this.#error = undefined;
    } catch (thrown) {
      this.#error = faultOf(thrown);
    } finally {
      this.#loading = false;
    }
  }
}

/** One store per page, built where the application builds its `PrusaConnect`. */
export const createLink = (client: LinkClient, options?: LinkOptions): Link =>
  new Link(client, options);
