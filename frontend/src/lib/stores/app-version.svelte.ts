// The build this server runs, as the footer names it: the release tag on a
// tagged build, VERSION and the commit otherwise (scripts/build-version.sh).
import type { PrusaConnect } from '@prusa/connect';

export type AppVersionClient = Pick<PrusaConnect, 'getHealth'>;

export class AppVersion {
  readonly #client: AppVersionClient;
  #version = $state<string | undefined>(undefined);
  #flight: Promise<void> | undefined;

  constructor(client: AppVersionClient) {
    this.#client = client;
  }

  /** Undefined until the server has answered, and after it refused. */
  get version(): string | undefined {
    return this.#version;
  }

  load(): Promise<void> {
    this.#flight ??= this.#client
      .getHealth()
      .then(
        (health) => {
          this.#version = health.version;
        },
        () => {
          // A footer without a version is still a footer.
        },
      )
      .finally(() => {
        this.#flight = undefined;
      });

    return this.#flight;
  }
}
