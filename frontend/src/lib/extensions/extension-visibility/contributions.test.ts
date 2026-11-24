import { SLOT_KEYS } from '@prusa/connect-ui/slots';
import { describe, expect, it } from 'vitest';

import type { ResourceInstance } from '@prusa/connect-ui/data';

import { contributionsOf } from './contributions';

// The slots the core lets an operator hide that this screen does not offer
// yet. A slot in neither this list nor the screen's own fails the test below,
// so a new slot is hideable in the change that adds it (ADR-20261021-3dp
// rule 4). Shrink this list as the screen offers them.
const NOT_OFFERED: readonly string[] = [
  'file_view',
  'file_tab',
  'camera_section',
  'camera_view',
  'device_header',
  'device_row',
  'device_file_action',
  'device_upload_action',
];

describe('the slots an operator may hide an entry of', () => {
  it('offers every section and action slot, or names it as not offered yet', () => {
    const keys = Object.values(SLOT_KEYS);
    const instance = {
      ui: {
        contributes: Object.fromEntries(
          keys.map((key) => [key, [{ id: 'one', title: 'One' }]]),
        ),
      },
    } as unknown as ResourceInstance;

    const offered = new Set(
      contributionsOf(instance).map((row) => row.key.split('/')[0]),
    );

    for (const key of keys) {
      if (NOT_OFFERED.includes(key)) continue;
      expect(offered, `${key} cannot be hidden`).toContain(key);
    }
  });
});
