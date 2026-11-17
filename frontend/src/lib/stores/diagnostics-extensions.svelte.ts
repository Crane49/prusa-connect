// The installed extensions that add files to a diagnostics bundle, as the
// Diagnostics room names them beside the choice to include them (ADR-20261019-nrq).
import type { DiagnosticsExtensionsClient } from '@prusa/connect-ui/data';
import { readDiagnosticsExtensions } from '@prusa/connect-ui/data';

export class DiagnosticsExtensions {
  readonly #client: DiagnosticsExtensionsClient;
  #titles = $state.raw<readonly string[] | undefined>(undefined);
  #error = $state<unknown>(undefined);
  #flight: Promise<void> | undefined;

  constructor(client: DiagnosticsExtensionsClient) {
    this.#client = client;
  }

  /** Undefined until the server has answered; empty when no extension adds files. */
  get titles(): readonly string[] | undefined {
    return this.#titles;
  }

  /** What the last read was refused with, such as a viewer who is not an admin. */
  get error(): unknown {
    return this.#error;
  }

  get loading(): boolean {
    return this.#flight !== undefined;
  }

  load(): Promise<void> {
    this.#flight ??= readDiagnosticsExtensions(this.#client)
      .then(
        (titles) => {
          this.#titles = titles;
          this.#error = undefined;
        },
        (thrown: unknown) => {
          // The bundle is still offered without the choice, so the refusal
          // costs the person only the extension files.
          this.#error = thrown;
        },
      )
      .finally(() => {
        this.#flight = undefined;
      });

    return this.#flight;
  }
}
