import { describe, expect, it } from 'vitest';

import { fileRefOf, parseFileRef } from './file-ref';

describe('a file reference', () => {
  it('joins the storage and the path with one colon', () => {
    expect(fileRefOf('local', 'models/bracket.bgcode')).toBe(
      'local:models/bracket.bgcode',
    );
  });

  // A path may hold colons of its own; a storage name never does.
  it('reads back the storage and the path it was made from', () => {
    const ref = fileRefOf('usb', 'a:b/c.gcode');

    expect(parseFileRef(ref)).toEqual({ storage: 'usb', path: 'a:b/c.gcode' });
  });

  it('is no reference without a storage, a path or a colon', () => {
    expect(parseFileRef('bracket.bgcode')).toBeUndefined();
    expect(parseFileRef(':bracket.bgcode')).toBeUndefined();
    expect(parseFileRef('local:')).toBeUndefined();
    expect(parseFileRef('')).toBeUndefined();
  });
});
