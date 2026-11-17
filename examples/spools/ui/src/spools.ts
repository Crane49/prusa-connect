// The service layer: the module's own routes, reached through the host's
// client (svelte-data.md, frontend-structure.md).
//
// `bind` comes from `spools-client`, which `cmd/generate-client-sdk -module`
// wrote from examples/spools/openapi.json. It attaches one method per
// operation onto the transport of a `@prusa/connect` client, so a call
// carries that client's credential and goes to the rebased path under
// /api/v1/modules/com.github.aux.acme.spools/ (ADR-20260909-c7x rule 9).
//
// The client the host hands over is in session mode, so the credential is the
// cookie the browser already holds. Nothing here names a URL, a header or a
// token, and there is no argument in which one could be named (ADR-20260911-4p6 rules
// 18 and 20).
import type { ModuleHost } from '@prusa/connect-ui/slots';
import { bind, type components } from 'spools-client';

/** One reel on the shelf, as the module's own fragment declares it. */
export type Spool = components['schemas']['Spool'];

/** What the store needs of the module's routes. A test hands in a fake. */
export interface SpoolsService {
  list(signal?: AbortSignal): Promise<readonly Spool[]>;
  add(row: Spool, signal?: AbortSignal): Promise<Spool>;
}

/** The two routes, bound onto the client the host handed this module. */
export const spoolsOn = (host: ModuleHost): SpoolsService => {
  const routes = bind(host.client);

  return {
    list: async (signal) => (await routes.listSpools({}, signal)).spools,
    add: async (row, signal) =>
      (await routes.createSpool({ body: row }, signal)).spool,
  };
};
