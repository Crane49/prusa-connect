import { describe, expect, it } from 'vitest';

import { MODULE_ICONS } from './generated';
import { DEFAULT_MODULE_ICON, moduleIcon } from './navigation';

describe('the icon a module page names', () => {
  it('is the listed icon for a listed name', () => {
    expect(moduleIcon('spool')).toBe(MODULE_ICONS.spool);
  });

  it('is the default for no name, an unknown name and an inherited key', () => {
    expect(moduleIcon(undefined)).toBe(DEFAULT_MODULE_ICON);
    expect(moduleIcon('no-such-icon')).toBe(DEFAULT_MODULE_ICON);
    expect(moduleIcon('constructor')).toBe(DEFAULT_MODULE_ICON);
    expect(moduleIcon('toString')).toBe(DEFAULT_MODULE_ICON);
  });
});
