// The service layer: this module's routes, typed from its own OpenAPI
// fragment, bound onto the client the host handed over. Every request goes
// through it, so the core authorizes each one as the person on the page.
import type { ModuleHost } from '@prusa/connect-ui/slots';
import {
  bind,
  type components,
  type Operations,
} from 'buddy-debug-logs-client';

export type LogsApi = Operations;
export type Status = components['schemas']['Status'];
export type StreamStatus = components['schemas']['StreamStatus'];
export type Source = components['schemas']['Source'];
export type LogLine = components['schemas']['LogLine'];
export type MetricName = components['schemas']['MetricName'];
export type SeriesPoint = components['schemas']['SeriesPoint'];
export type SetupAccepted = components['schemas']['SetupAccepted'];

/** The module's routes, on the host's client. */
export const apiOn = (host: Pick<ModuleHost, 'client'>): LogsApi =>
  bind(host.client);

/** The core's own answer about the app, which names the address printers dial. */
export type AppClient = Pick<ModuleHost['client'], 'getUiSettings'>;
