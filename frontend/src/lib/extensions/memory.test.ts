import { describe, expect, it } from 'vitest';

import { memoryEstimate, offerBudget, plannedBytes } from './memory';

const MIB = 1024 * 1024;

describe('memoryEstimate', () => {
  it('uses the 128 MiB default when the offer recommends nothing', () => {
    for (const none of [0, undefined]) {
      const { budgetBytes, source } = offerBudget(none);
      expect(
        memoryEstimate({
          budgetBytes,
          source,
          pool: undefined,
          heldBytes: 0,
        }),
      ).toMatchObject({ mib: 128, source: 'default', overPool: false });
    }
  });

  it('is over the pool when the author budget does not fit beside what is committed', () => {
    const { budgetBytes, source } = offerBudget(2560);
    const estimate = memoryEstimate({
      budgetBytes,
      source,
      pool: { committed_bytes: 1024 * MIB, pool_bytes: 3072 * MIB },
      heldBytes: 0,
    });
    expect(estimate).toMatchObject({
      mib: 2560,
      source: 'author',
      overPool: true,
      committedMiB: 3584,
      poolMiB: 3072,
    });
  });

  it('does not add the budget an enabled module already holds twice', () => {
    expect(
      memoryEstimate({
        budgetBytes: 256 * MIB,
        source: 'author',
        pool: { committed_bytes: 3000 * MIB, pool_bytes: 3072 * MIB },
        heldBytes: 256 * MIB,
      }),
    ).toMatchObject({ overPool: false, committedMiB: 3000 });
  });

  it('counts only the raise when an enabled module gets a bigger budget', () => {
    expect(
      memoryEstimate({
        budgetBytes: 2560 * MIB,
        source: 'author',
        pool: { committed_bytes: 3000 * MIB, pool_bytes: 3072 * MIB },
        heldBytes: 256 * MIB,
      }),
    ).toMatchObject({ overPool: true, committedMiB: 5304 });
  });

  it('never reads an unknown pool as zero', () => {
    const estimate = memoryEstimate({
      budgetBytes: 128 * MIB,
      source: 'default',
      pool: { committed_bytes: 512 * MIB },
      heldBytes: 0,
    });
    expect(estimate.overPool).toBe(false);
    expect(estimate).not.toHaveProperty('poolMiB');
  });
});

describe('plannedBytes', () => {
  it('adds nothing for a release that runs no process', () => {
    expect(
      plannedBytes([
        { memory_mib: 256, process: true },
        { process: false },
        { process: true },
      ]),
    ).toBe((256 + 128) * MIB);
  });
});
