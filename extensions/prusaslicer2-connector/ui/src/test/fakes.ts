// What the stories and the tests draw instead of the server: the module's
// English texts and a host whose client mints a credential.
import {
  locale,
  registerCatalogue,
  translateFor,
} from '@prusa/connect-ui/i18n';
import type { ModuleHost } from '@prusa/connect-ui/slots';

import type { Keep } from '../keep';
import english from '../locales/en.json';
import type { Translate } from '../views/prusalink-section';

export const NAMESPACE = 'com.github.aux.prusa3d.prusaslicer2-connector';
registerCatalogue(NAMESPACE, 'en', english);
locale.set('en');

/** The module's English texts, with the params filled in. */
export const t: Translate = translateFor(NAMESPACE);

/** A token in the shape this server mints. */
export const TOKEN =
  'pcl_STTmTqUm4tEFVfJORGKSkY_abEW4e9a6ft2HJGriETLQPM4M3HAq7wiZUmQIdFH4xL2tCY7j';

/** One credential request the fake client received. */
export interface Minted {
  name: string;
  scopes: string[] | undefined;
}

/** One write of the keep route the fake client received. */
export interface Kept {
  printer: string;
  keep: Keep;
}

/**
 * A host with one printer and one storage, `local`, holding the folder
 * `prints`. Its client answers createCredential with TOKEN, or throws refusal
 * when one is given, and serves the module's keep route from memory, or
 * refuses its writes with keepRefusal. `minted` and `kept` record each
 * request.
 */
export const stubHost = (
  options: {
    refusal?: Error;
    hold?: Promise<void>;
    keep?: Keep;
    keepRefusal?: Error;
    seenAt?: string | null;
    mayChange?: boolean;
  } = {},
): {
  host: ModuleHost;
  minted: Minted[];
  kept: Kept[];
  reads: () => number;
} => {
  const minted: Minted[] = [];
  const kept: Kept[] = [];
  const controller = new AbortController();
  let held: Keep = options.keep ?? { storage: '', folder: '' };
  let read = 0;

  const client = {
    createCredential: async (request: {
      body: { name: string; scopes?: string[] };
    }) => {
      minted.push({ name: request.body.name, scopes: request.body.scopes });
      await options.hold;
      if (options.refusal !== undefined) throw options.refusal;

      return { token: TOKEN };
    },
    listStorages: () =>
      Promise.resolve({
        items: [{ name: 'local', display_name: 'Local', write: true }],
      }),
    listStorageFiles: () =>
      Promise.resolve({
        items: [{ name: 'prints', path: 'prints', folder: true }],
      }),
    transport: {
      call: (
        operation: { method: string; path: string },
        request: { path: { printer: string }; body?: Keep },
      ) => {
        if (operation.path.endsWith('/seen'))
          return Promise.resolve({ seen_at: options.seenAt ?? null });
        if (operation.method === 'GET') {
          read += 1;

          const may = options.mayChange ?? true;

          return Promise.resolve({
            ...(may ? held : { storage: '', folder: '' }),
            on: held.storage !== '',
            may_change: may,
            ...(may ? {} : { refused: 'operate' as const }),
          });
        }
        if (options.keepRefusal !== undefined)
          return Promise.reject(options.keepRefusal);

        const keep = request.body ?? { storage: '', folder: '' };
        kept.push({ printer: request.path.printer, keep });
        held = keep;

        return Promise.resolve(keep);
      },
    },
  };

  const host = {
    client,
    t,
    signal: controller.signal,
    pageHref: (path: string) => `/m/${NAMESPACE}/${path}`,
  } as unknown as ModuleHost;

  return { host, minted, kept, reads: () => read };
};
