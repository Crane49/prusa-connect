// Setting a printer up to send here: the address the printer reaches this
// server at, and the one request that sends it M334.
//
// The module sends the G-code as the person on the page, and the printer
// asks a person on its display before it changes anything. Nothing here
// answers that question.
import type { AppClient, LogsApi, SetupAccepted, Status } from './api';
import { describeFault } from '@prusa/connect-ui/i18n';
import { printerHost } from './presentation';

/** The firmware's own default ports. */
export const FIRMWARE_PORTS = { syslog: 13514, metrics: 8514 } as const;

/** The refusal that means the person may not send G-code to this printer. */
export const GCODE_NOT_ALLOWED = 'buddy-debug-logs/GCODE_NOT_ALLOWED';

/** The port each stream is granted, or the firmware's default where none is. */
export const portsOf = (
  status: Status | undefined,
): { syslog: number; metrics: number } => {
  const port = (stream: 'syslog' | 'metrics'): number =>
    status?.streams.find((one) => one.stream === stream)?.listener.port ??
    FIRMWARE_PORTS[stream];

  return { syslog: port('syslog'), metrics: port('metrics') };
};

export interface SetupChoice {
  readonly deviceId: string;
  readonly host: string;
  readonly syslog: boolean;
  readonly metrics: boolean;
}

export class SetupStore {
  readonly #api: Pick<LogsApi, 'setUpPrinter'>;
  readonly #app: AppClient;
  readonly #pageHost: string;
  #host = $state<string | undefined>(undefined);
  #sending = $state(false);
  #sent = $state.raw<SetupAccepted | undefined>(undefined);
  #fault = $state.raw<unknown>(undefined);
  #flight: Promise<void> | undefined;

  constructor(
    api: Pick<LogsApi, 'setUpPrinter'>,
    app: AppClient,
    pageHost: string,
  ) {
    this.#api = api;
    this.#app = app;
    this.#pageHost = pageHost;
  }

  /** The address to write into the printer, once it is known. */
  get host(): string | undefined {
    return this.#host;
  }

  get sending(): boolean {
    return this.#sending;
  }

  /** What the module sent, after a request it accepted. */
  get sent(): SetupAccepted | undefined {
    return this.#sent;
  }

  get fault(): unknown {
    return this.#fault;
  }

  /** Whether the refusal means the person may not send G-code at all. */
  get notAllowed(): boolean {
    return (
      this.#fault !== undefined &&
      describeFault(this.#fault).code === GCODE_NOT_ALLOWED
    );
  }

  /**
   * Reads the address printers dial from the app's settings, once. The page's
   * own host stands in when the server names none or does not answer.
   */
  loadHost(): Promise<void> {
    this.#flight ??= this.#app.getUiSettings().then(
      (settings) => {
        this.#host = printerHost(settings.printer_address, this.#pageHost);
      },
      () => {
        this.#host = printerHost(undefined, this.#pageHost);
      },
    );

    return this.#flight;
  }

  /** Forgets the last answer and reads the address, as the dialog opens. */
  open(): void {
    this.#sent = undefined;
    this.#fault = undefined;
    void this.loadHost();
  }

  /** Sends the printer its new setting. */
  async send(
    choice: SetupChoice,
    ports: { syslog: number; metrics: number },
  ): Promise<void> {
    if (this.#sending) return;
    this.#sending = true;
    this.#fault = undefined;
    this.#sent = undefined;
    try {
      this.#sent = await this.#api.setUpPrinter({
        path: { device_id: choice.deviceId },
        body: {
          host: choice.host.trim(),
          syslog: choice.syslog,
          metrics: choice.metrics,
          syslog_port: ports.syslog,
          metrics_port: ports.metrics,
        },
      });
    } catch (thrown) {
      this.#fault = thrown;
    } finally {
      this.#sending = false;
    }
  }
}
