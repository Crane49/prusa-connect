// What a person typed survives a reload, and the secrets never reach the
// storage that outlives the tab (ADR-20261015-ejz rule 30).
import { describe, expect, it } from 'vitest';

import type { RememberedStorage } from '../../remembered';
import { DRAFT_KEY, SECRET_KEY, SetupDraft } from './setup-draft.svelte';

const memory = (): RememberedStorage & { map: Map<string, string> } => {
  const map = new Map<string, string>();

  return {
    map,
    getItem: (key) => map.get(key) ?? null,
    setItem: (key, value) => {
      map.set(key, value);
    },
  };
};

describe('the draft', () => {
  it('reads back every answer after a reload', () => {
    const local = memory();
    const tab = memory();
    const first = new SetupDraft(local, tab);

    first.step = 'identity';
    first.path = 'advanced';
    first.username = 'owner';
    first.password = 'correct horse';
    first.code = 'K7QD-3M9X-TP2A';
    first.answer('core.site', 'workshop');
    first.mqttOn = true;
    first.mqttPassword = 'broker secret';

    const reloaded = new SetupDraft(local, tab);

    expect(reloaded.step).toBe('identity');
    expect(reloaded.path).toBe('advanced');
    expect(reloaded.username).toBe('owner');
    expect(reloaded.password).toBe('correct horse');
    expect(reloaded.code).toBe('K7QD-3M9X-TP2A');
    expect(reloaded.answers).toEqual({ 'core.site': 'workshop' });
    expect(reloaded.mqttOn).toBe(true);
    expect(reloaded.mqttPassword).toBe('broker secret');
  });

  it('keeps every secret out of the storage that outlives the tab', () => {
    const local = memory();
    const draft = new SetupDraft(local, memory());

    draft.password = 'correct horse';
    draft.confirmation = 'correct horse';
    draft.code = 'K7QD-3M9X-TP2A';
    draft.mqttPassword = 'broker secret';
    draft.username = 'owner';

    const kept = local.map.get(DRAFT_KEY) ?? '';
    expect(kept).not.toContain('correct horse');
    expect(kept).not.toContain('K7QD');
    expect(kept).not.toContain('broker secret');
    expect(kept).toContain('owner');
  });

  it('starts again from a document somebody broke', () => {
    const local = memory();
    local.setItem(
      DRAFT_KEY,
      '{"step":"nowhere","answers":{"core.site":42},"mode":"x"}',
    );
    const tab = memory();
    tab.setItem(SECRET_KEY, 'not json');

    const draft = new SetupDraft(local, tab);

    expect(draft.step).toBe('welcome');
    expect(draft.mode).toBe('required');
    expect(draft.answers).toEqual({});
    expect(draft.password).toBe('');
  });

  it('remembers every step opened, once each, across a reload', () => {
    const local = memory();
    const draft = new SetupDraft(local, memory());

    draft.step = 'code';
    draft.step = 'path';
    draft.step = 'code';

    expect(new SetupDraft(local, memory()).visited).toEqual([
      'welcome',
      'code',
      'path',
    ]);
  });

  it('counts only its own step as seen in a draft that kept no steps', () => {
    const local = memory();
    local.setItem(
      DRAFT_KEY,
      '{"step":"summary","visited":["code","nowhere",3]}',
    );
    const older = memory();
    older.setItem(DRAFT_KEY, '{"step":"storage"}');

    expect(new SetupDraft(local, memory()).visited).toEqual([
      'code',
      'summary',
    ]);
    expect(new SetupDraft(older, memory()).visited).toEqual(['storage']);
  });

  it('forgets everything once the server has recorded the decision', () => {
    const local = memory();
    const tab = memory();
    const draft = new SetupDraft(local, tab);
    draft.username = 'owner';
    draft.password = 'correct horse';
    draft.step = 'summary';

    draft.clear();

    expect(new SetupDraft(local, tab).visited).toEqual(['welcome']);
    expect(new SetupDraft(local, tab).username).toBe('');
    expect(new SetupDraft(local, tab).password).toBe('');
  });
});
