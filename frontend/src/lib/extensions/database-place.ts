// Where an installed module's own database is, as one sentence. The server
// names the Postgres server and the database and never the login.
import type { components } from '@prusa/connect';
import type { Translate } from '@prusa/connect-ui/i18n';

type ResourceDatabase = components['schemas']['ResourceDatabase'];

/** The sentence for the database a module holds, and '' for none. */
export const databasePlace = (
  held: ResourceDatabase | null | undefined,
  t: Translate,
): string => {
  if (held === undefined || held === null) return '';
  if (held.engine === 'sqlite') {
    return t('app.settings.extensions.database.sqlite');
  }

  const server = held.server ?? '';

  return held.provisioned === true
    ? t('app.settings.extensions.database.provisioned', { server })
    : t('app.settings.extensions.database.postgres', { server });
};

/** The sentence for the schema version a running module reported, and '' for none. */
export const databaseSchema = (
  held: ResourceDatabase | null | undefined,
  t: Translate,
): string => {
  const schema = held?.schema;
  if (schema === undefined) return '';

  return t('app.settings.extensions.database.schema', {
    version: String(schema.version),
  });
};
