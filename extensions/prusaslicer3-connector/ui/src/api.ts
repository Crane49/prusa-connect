// The service layer: this module's routes, typed from its own OpenAPI
// fragment, bound onto the client the host handed over. Every request goes
// through it, so the core authorizes each one as the person on the page.
import type { ModuleHost } from '@prusa/connect-ui/slots';
import {
  bind,
  type components,
  type Operations,
} from 'prusaslicer3-connector-client';

export type PrusaSlicerApi = Pick<Operations, 'listSlicerPrinters'>;
export type SlicerPrinter = components['schemas']['SlicerPrinter'];
export type SlicerTool = components['schemas']['SlicerTool'];

/** The module's routes, on the host's client. */
export const apiOn = (host: Pick<ModuleHost, 'client'>): PrusaSlicerApi =>
  bind(host.client);

/** Translates one of this module's texts. */
export type Translate = ModuleHost['t'];
