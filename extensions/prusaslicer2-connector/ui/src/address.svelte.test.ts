import { describe, expect, it } from 'vitest';

import { namespaceOf, printerAddress } from './address';

describe('the printer address', () => {
  it('reads the namespace out of the address of a page of this module', () => {
    expect(
      namespaceOf('/m/com.github.aux.prusa3d.prusaslicer2-connector/x'),
    ).toBe('com.github.aux.prusa3d.prusaslicer2-connector');
    expect(namespaceOf('/elsewhere')).toBeUndefined();
  });

  it('is the printer under the mount, ending in a slash', () => {
    expect(printerAddress('http://192.168.1.5:8080/', 'ns', 'mk4 01')).toBe(
      'http://192.168.1.5:8080/api/v1/modules/ns/printers/mk4%2001/',
    );
  });
});
