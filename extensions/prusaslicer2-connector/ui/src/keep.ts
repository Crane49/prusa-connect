// Where one printer's uploads are also saved on this server and when
// PrusaSlicer last reached it: the module's own /printers/{printer}/keep and
// /printers/{printer}/seen, sent through the client the host
// handed this module, so the call carries the person's session.
import type { Operation, PrusaConnect } from '@prusa/connect';

/** The storage and folder of this server. An empty storage saves nothing. */
export interface Keep {
  readonly storage: string;
  readonly folder: string;
}

/** When PrusaSlicer last reached the printer, null when it has not yet. */
export interface Seen {
  readonly seen_at: string | null;
}

/** The setting as GET answers it, with whether the person may change it. */
export interface KeepState extends Keep {
  readonly on: boolean;
  readonly may_change: boolean;
  /**
   * Why the person may not change it: `operate` is a person who may not
   * operate the printer, and `check` a module not granted its operate check,
   * so nobody may. The storage and folder are then empty.
   */
  readonly refused?: 'operate' | 'check';
}

/** What the section needs of the routes. */
export interface KeepService {
  read(printer: string): Promise<KeepState>;
  write(printer: string, keep: Keep): Promise<Keep>;
  seen(printer: string): Promise<Seen>;
}

const PATH = '/api/v1/modules/{namespace}/printers/{printer}/keep';

const READ: Operation = {
  id: 'linkKeep',
  method: 'GET',
  path: PATH,
  response: 'json',
  anonymous: false,
};

const WRITE: Operation = {
  id: 'setLinkKeep',
  method: 'PUT',
  path: PATH,
  body: 'application/json',
  response: 'json',
  anonymous: false,
};

const SEEN: Operation = {
  id: 'linkSeen',
  method: 'GET',
  path: '/api/v1/modules/{namespace}/printers/{printer}/seen',
  response: 'json',
  anonymous: false,
};

/** The routes of the module installed under namespace. */
export const keepsOn = (
  client: Pick<PrusaConnect, 'transport'>,
  namespace: string,
): KeepService => ({
  read: (printer) =>
    client.transport.call<KeepState>(READ, { path: { namespace, printer } }),
  write: (printer, keep) =>
    client.transport.call<Keep>(WRITE, {
      path: { namespace, printer },
      body: keep,
    }),
  seen: (printer) =>
    client.transport.call<Seen>(SEEN, { path: { namespace, printer } }),
});
