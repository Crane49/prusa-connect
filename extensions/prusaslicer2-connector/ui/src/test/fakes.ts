// What the stories and the tests draw instead of the server: the module's
// English texts and a host whose client mints a credential.
import {
  locale,
  registerCatalogue,
  translateFor,
} from '@prusa/connect-ui/i18n';
import type { ModuleHost } from '@prusa/connect-ui/slots';

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

/**
 * A host with one printer. Its client answers createCredential with TOKEN,
 * or throws refusal when one is given. `minted` records each request.
 */
export const stubHost = (
  options: { refusal?: Error; hold?: Promise<void> } = {},
): { host: ModuleHost; minted: Minted[] } => {
  const minted: Minted[] = [];
  const controller = new AbortController();

  const client = {
    createCredential: async (request: {
      body: { name: string; scopes?: string[] };
    }) => {
      minted.push({ name: request.body.name, scopes: request.body.scopes });
      await options.hold;
      if (options.refusal !== undefined) throw options.refusal;

      return { token: TOKEN };
    },
  };

  const host = {
    client,
    t,
    signal: controller.signal,
    pageHref: (path: string) => `/m/${NAMESPACE}/${path}`,
  } as unknown as ModuleHost;

  return { host, minted };
};
