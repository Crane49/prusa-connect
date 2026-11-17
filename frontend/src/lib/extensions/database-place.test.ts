// The sentences the extension page says about a module's own database. The
// translation is a stand-in that writes the key and its values, so the case
// reads which sentence was chosen and what it was given.
import type { Translate } from '@prusa/connect-ui/i18n';
import { describe, expect, it } from 'vitest';

import { databasePlace, databaseSchema } from './database-place';

const t = ((key: string, values?: Record<string, string>): string =>
  `${key} ${JSON.stringify(values ?? {})}`) as Translate;

describe('database place', () => {
  it('names the server and the schema version the running module reported', () => {
    const held = {
      engine: 'postgres' as const,
      provisioned: true,
      server: 'db.lan:5432/module_notes',
      schema: { version: 4, compat: 3 },
    };

    expect(databasePlace(held, t)).toBe(
      'app.settings.extensions.database.provisioned {"server":"db.lan:5432/module_notes"}',
    );
    expect(databaseSchema(held, t)).toBe(
      'app.settings.extensions.database.schema {"version":"4"}',
    );
  });

  it('says nothing about a schema a module did not report', () => {
    expect(databaseSchema({ engine: 'sqlite' }, t)).toBe('');
    expect(databaseSchema(undefined, t)).toBe('');
  });
});
