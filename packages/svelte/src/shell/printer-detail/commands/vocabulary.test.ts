// The two key families this module builds rather than writes out.
import { describe, expect, it } from 'vitest';

import { CORE_CATALOGUES } from '../../../i18n/catalogues';
import {
  COMMAND_GROUPS,
  descriptionKeyOf,
  GROUP_LABEL_KEY,
  labelKeyOf,
  VERB_GROUP,
} from './vocabulary';

const english = CORE_CATALOGUES.en ?? {};

const family = (prefix: string): string[] =>
  Object.keys(english).filter((key) => key.startsWith(prefix));

describe('the command vocabulary', () => {
  it('has a catalogue line for every verb it draws', () => {
    const missing = Object.keys(VERB_GROUP).flatMap((verb) =>
      [labelKeyOf(verb), descriptionKeyOf(verb)].filter(
        (key) => key === undefined || english[key] === undefined,
      ),
    );

    expect(missing).toEqual([]);
  });

  it('holds no catalogue line no verb asks for', () => {
    const asked = new Set(
      Object.keys(VERB_GROUP).flatMap((verb) => [
        labelKeyOf(verb),
        descriptionKeyOf(verb),
      ]),
    );
    const orphans = [
      ...family('printerDetail.commands.verb.'),
      ...family('printerDetail.commands.detail.'),
    ].filter((key) => !asked.has(key));

    expect(orphans).toEqual([]);
  });

  it('puts every verb under a heading the catalogue names', () => {
    for (const verb of Object.keys(VERB_GROUP)) {
      const group = VERB_GROUP[verb];
      expect(COMMAND_GROUPS).toContain(group);
      expect(english[GROUP_LABEL_KEY[group ?? 'other']]).toBeDefined();
    }
  });
});
