// This server's health, for the Server room in Settings. The two reads are
// independent: the readiness route takes no credential, the connector rollup
// takes the caller's view of the printers. Each fails on its own, so each
// keeps its own fault.
import { type DataFault, faultOf } from './fault';
import {
  type ConnectorRollup,
  type HealthClient,
  readConnectorHealth,
  type ReadinessReport,
  readReadiness,
} from './services/health';

/** A shared store, so a class (svelte-5.md). */
export class ServerHealth {
  readonly #client: HealthClient;

  #readiness = $state.raw<ReadinessReport | undefined>(undefined);
  #connectors = $state.raw<readonly ConnectorRollup[]>([]);
  #readinessError = $state<DataFault | undefined>(undefined);
  #connectorsError = $state<DataFault | undefined>(undefined);
  #loading = $state(false);
  #loaded = $state(false);
  #flight: Promise<void> | undefined;
  // Counts the resets. A read that started before a reset records nothing.
  #resets = 0;

  constructor(client: HealthClient) {
    this.#client = client;
  }

  /** Read-only by design: both answers are the server's (svelte-5.md). */
  get readiness(): ReadinessReport | undefined {
    return this.#readiness;
  }

  get connectors(): readonly ConnectorRollup[] {
    return this.#connectors;
  }

  get readinessError(): DataFault | undefined {
    return this.#readinessError;
  }

  get connectorsError(): DataFault | undefined {
    return this.#connectorsError;
  }

  get loading(): boolean {
    return this.#loading;
  }

  /** True once both reads have answered, with a report or a refusal. */
  get loaded(): boolean {
    return this.#loaded;
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

  /** Forgets both answers, for a person who signed out. */
  reset(): void {
    this.#resets += 1;
    this.#flight = undefined;
    this.#readiness = undefined;
    this.#connectors = [];
    this.#readinessError = undefined;
    this.#connectorsError = undefined;
    this.#loading = false;
    this.#loaded = false;
  }

  async #read(): Promise<void> {
    const resets = this.#resets;
    this.#loading = true;

    const [readiness, connectors] = await Promise.allSettled([
      readReadiness(this.#client),
      readConnectorHealth(this.#client),
    ]);
    if (resets !== this.#resets) return;

    if (readiness.status === 'fulfilled') {
      this.#readiness = readiness.value;
      this.#readinessError = undefined;
    } else {
      this.#readinessError = faultOf(readiness.reason);
    }

    if (connectors.status === 'fulfilled') {
      this.#connectors = connectors.value;
      this.#connectorsError = undefined;
    } else {
      this.#connectorsError = faultOf(connectors.reason);
    }

    this.#loading = false;
    this.#loaded = true;
  }
}
