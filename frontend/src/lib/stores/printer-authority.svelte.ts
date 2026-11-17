// The certificate a printer loads from its USB drive as connect.der, as the
// add-printer page offers it.
import type { PrinterAuthorityClient } from '@prusa/connect-ui/data';
import { readPrinterAuthority } from '@prusa/connect-ui/data';

export class PrinterAuthority {
  readonly #client: PrinterAuthorityClient;
  #file = $state.raw<Blob | null | undefined>(undefined);
  #flight: Promise<void> | undefined;

  constructor(client: PrinterAuthorityClient) {
    this.#client = client;
  }

  /** Undefined until the server has answered; null when it has no certificate to give. */
  get file(): Blob | null | undefined {
    return this.#file;
  }

  load(): Promise<void> {
    this.#flight ??= readPrinterAuthority(this.#client)
      .then(
        (file) => {
          this.#file = file;
        },
        () => {
          // Offered as missing, with the way to make one by hand.
          this.#file = null;
        },
      )
      .finally(() => {
        this.#flight = undefined;
      });

    return this.#flight;
  }
}
