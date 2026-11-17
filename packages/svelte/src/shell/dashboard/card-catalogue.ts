// Every kind of card the page can draw, from the core and from the modules.
//
// The slot registry holds the declarations a module's manifest made, before
// its bundle loads, and the components a bundle registered after it loaded
// (ADR-20261008-sww rules 15 to 17). This reads both for the dashboard: the gallery
// lists the declarations, the grid sizes a card from them, and a card is
// drawn by the component its type registered.
//
// The declarations come from the listing, which `discover` reads once for
// the page and which imports no bundle. A module's bundle is loaded only when
// a card of that module is on the dashboard drawn (ADR-20261008-sww rule 25).
import type { CellSize } from '../../domain';
import { t } from '../../i18n';
import {
  type CardDeclaration,
  CORE_CARD_NAMESPACE,
  type ModuleLoader,
  type RegisteredCard,
  type SlotRegistry,
} from '../../slots';

/** One kind of card as the gallery offers it. */
export interface CardKind {
  readonly type: string;
  readonly namespace: string;
  readonly declaration: CardDeclaration;
}

const CORE_NAMESPACE = CORE_CARD_NAMESPACE;

/** The size a card of an unknown type is drawn at and may shrink to. */
const ONE_CELL: CellSize = { w: 1, h: 1 };

/** The namespace a card type names, the part before its slash. */
export const namespaceOf = (type: string): string => {
  const slash = type.indexOf('/');

  return slash < 0 ? '' : type.slice(0, slash);
};

/** What the catalogue needs of the module loader. */
export type CardLoader = Pick<
  ModuleLoader,
  'activateFor' | 'discover' | 'discovered' | 'unread'
>;

export class CardCatalogue {
  readonly #registry: SlotRegistry;
  readonly #loader: CardLoader;

  constructor(registry: SlotRegistry, loader: CardLoader) {
    this.#registry = registry;
    this.#loader = loader;
  }

  /**
   * True once the modules' listing was read. Before that, a type no module
   * declares yet may still be declared by one, so a card of it is loading.
   */
  get listed(): boolean {
    return this.#loader.discovered;
  }

  /**
   * True when the modules' listing could not be read, so a type no module
   * declares is not known to be missing. `discover` reads it again.
   */
  get unread(): boolean {
    return this.#loader.unread;
  }

  /**
   * Reads which cards the installed modules declare, without loading any
   * bundle. The screen calls it once, as it opens, so the gallery and the
   * grid know every card's size before the first module runs.
   */
  discover(): Promise<void> {
    return this.#loader.discover();
  }

  /** Every kind of card a person may add, the core's first, then by title. */
  get kinds(): readonly CardKind[] {
    return [...this.#registry.declaredCards()].sort((a, b) => {
      if (a.namespace === CORE_NAMESPACE && b.namespace !== CORE_NAMESPACE)
        return -1;
      if (b.namespace === CORE_NAMESPACE && a.namespace !== CORE_NAMESPACE)
        return 1;

      return a.namespace === CORE_NAMESPACE
        ? 0
        : a.declaration.title.localeCompare(b.declaration.title);
    });
  }

  /** The declaration of one type, when an installed module declares it. */
  kind(type: string): CardKind | undefined {
    return this.#registry.declaredCards().find((kind) => kind.type === type);
  }

  /** The component of one type, once its bundle registered it. */
  card(type: string): RegisteredCard | undefined {
    return this.#registry.card(type);
  }

  /**
   * What a person calls the module a card comes from: the title of its page,
   * or the last word of its namespace.
   */
  moduleOf(namespace: string): string {
    return (
      this.#registry.moduleTitle(namespace) ??
      namespace.slice(namespace.lastIndexOf('.') + 1)
    );
  }

  /** The size a new card of a type starts at. */
  sizeOf(type: string): CellSize {
    return this.kind(type)?.declaration.size ?? ONE_CELL;
  }

  /** The smallest a card of a type may be. */
  leastOf(type: string): CellSize {
    return this.kind(type)?.declaration.least ?? ONE_CELL;
  }

  /** Loads the bundles of the modules whose cards these types name. */
  async load(types: readonly string[]): Promise<void> {
    const namespaces = [
      ...new Set(
        types
          .map(namespaceOf)
          .filter((space) => space !== CORE_NAMESPACE && space !== ''),
      ),
    ];
    if (namespaces.length > 0) await this.#loader.activateFor(namespaces);
  }

  /**
   * How the words of a type's declaration are read. A core card names keys
   * of the library's catalogue. A module's card names its own words, which
   * are drawn as they are.
   */
  wordsOf(type: string): (text: string) => string {
    return namespaceOf(type) === CORE_NAMESPACE
      ? (key) => t(key)
      : (text) => text;
  }

  /** What a card of a type is called, for a control that names it. */
  titleOf(type: string): string {
    const kind = this.kind(type);

    return kind === undefined
      ? type
      : this.wordsOf(type)(kind.declaration.title);
  }
}
