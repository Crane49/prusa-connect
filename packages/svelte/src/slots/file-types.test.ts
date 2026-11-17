import { describe, expect, it } from 'vitest';

import { matchesFileRef, matchesFileType } from './file-types';

describe('matchesFileType', () => {
  it('draws an entry with no types for every file', () => {
    expect(matchesFileType(undefined, 'a.gcode')).toBe(true);
    expect(matchesFileType([], 'a.gcode')).toBe(true);
  });

  it('matches the extension without regard to case, and only at the end', () => {
    expect(matchesFileType(['.3mf'], 'PLATE.3MF')).toBe(true);
    expect(matchesFileType(['.3mf'], 'plate.3mf.gcode')).toBe(false);
    expect(matchesFileType(['.3mf'], '.3mf')).toBe(false);
  });

  it('reads the name out of a file reference', () => {
    expect(matchesFileRef(['.demo'], 'bench:folder/box.demo')).toBe(true);
    expect(matchesFileRef(['.demo'], 'bench:folder.demo/box.gcode')).toBe(
      false,
    );
    expect(matchesFileRef(['.demo'], 'not a reference')).toBe(false);
  });
});
