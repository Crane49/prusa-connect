import { describe, expect, it } from 'vitest';

import { hiddenEntries } from './module-hidden';

describe('the entries a module has hidden', () => {
  it('joins the operator list and the module list, each entry once', () => {
    expect(
      hiddenEntries({
        hidden_contributions: ['pages/plan', 'pages/customers'],
        ui: { hidden: ['pages/customers', 'file_action/plan'] },
      }),
    ).toEqual(['pages/plan', 'pages/customers', 'file_action/plan']);
  });

  it('is the operator list while the module hides nothing', () => {
    const operator = ['pages/plan'];

    expect(hiddenEntries({ hidden_contributions: operator, ui: {} })).toBe(
      operator,
    );
    expect(hiddenEntries({})).toEqual([]);
  });
});
