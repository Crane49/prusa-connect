import {
  type DeviceModelEntry,
  type DeviceModelsClient,
  readDeviceModels,
} from './services/device-models';

// A device reports its model in its own spelling, so a row is found the way
// the server's profile lookup finds one: by the model name with case and every
// character that is not an ASCII letter or digit left out, and then by the
// device type code as written, which some firmware reports where a model name
// is expected. The fold is `connectorapi.FoldModel` on the server, step for
// step: drop first, then lowercase. The server keeps one row per folded
// spelling, so a name finds at most one row.
const fold = (model: string): string =>
  model.replace(/[^A-Za-z0-9]/g, '').toLowerCase();

/**
 * The device models this build can name, read once per session: the Prusa
 * models a capability profile covers and the models its drivers declared.
 *
 * A screen asks it what a model is called and what it looks like. A page
 * never asks it how many heads a printer has: that is the printer's tool tree
 * (ADR-20261023-gna rule 12). A server that cannot answer leaves the list empty, and
 * every model then reads as one the vocabulary does not name.
 *
 * A shared store, so a class (svelte-5.md). The client comes in through the
 * constructor.
 */
export class DeviceModels {
  readonly #client: DeviceModelsClient;
  #models = $state.raw<readonly DeviceModelEntry[]>([]);
  #asked = $state(false);
  #error = $state<Error | undefined>(undefined);
  #flight: Promise<void> | undefined;

  constructor(client: DeviceModelsClient) {
    this.#client = client;
  }

  /** True once a read has finished, whether it answered or failed. */
  get asked(): boolean {
    return this.#asked;
  }

  /** Why the read failed, when it did. */
  get error(): Error | undefined {
    return this.#error;
  }

  /**
   * The row for one machine's own spelling of its model, or for the code some
   * firmware reports where a model name is expected. Undefined for a spelling
   * the vocabulary has no row for, and before the read lands.
   */
  entryOf(spelling: string | undefined): DeviceModelEntry | undefined {
    return this.#find(spelling);
  }

  /**
   * The name a model is sold under, by the spelling the device reports, or
   * `undefined` for a model the vocabulary does not name.
   */
  displayName(model: string | undefined): string | undefined {
    return this.#find(model)?.display_name;
  }

  /**
   * Where the server serves a drawing of a model, by the spelling the device
   * reports, or `undefined` for a model whose driver declared none.
   */
  artwork(model: string | undefined): string | undefined {
    return this.#find(model)?.artwork;
  }

  /**
   * What a printer icon with no artwork of its own shows for a model: the name
   * the vocabulary gives it, and its drawing when a driver declared one. That
   * is most often a model a driver declared (ADR-20261016-qrp), and it can be a Prusa
   * model this library has no drawing of. The application hands this to the
   * icons as their lookup.
   */
  declared(reported: string): { name: string; src?: string } | undefined {
    const entry = this.#find(reported);
    if (entry === undefined) return undefined;

    return entry.artwork === undefined
      ? { name: entry.display_name }
      : { name: entry.display_name, src: entry.artwork };
  }

  // Every accessor finds its row here, so two of them never disagree about
  // which row a spelling names.
  #find(spelling: string | undefined): DeviceModelEntry | undefined {
    if (spelling === undefined) return undefined;

    const key = fold(spelling);
    if (key === '') return undefined;

    return (
      this.#models.find((entry) => fold(entry.model) === key) ??
      this.#models.find((entry) => entry.device_type_id === spelling.trim())
    );
  }

  /**
   * Reads the vocabulary. A call while one is in flight joins it, and a call
   * after an answer landed does nothing. A failed read is asked again.
   */
  read(): Promise<void> {
    if (this.#asked && this.#error === undefined) return Promise.resolve();

    this.#flight ??= this.#read().finally(() => {
      this.#flight = undefined;
    });

    return this.#flight;
  }

  async #read(): Promise<void> {
    try {
      const answer = await readDeviceModels(this.#client);
      this.#models = answer.items ?? [];
      this.#error = undefined;
    } catch (thrown) {
      this.#models = [];
      this.#error =
        thrown instanceof Error ? thrown : new Error(String(thrown));
    } finally {
      this.#asked = true;
    }
  }
}
