import { expect, test } from 'vitest';

import {
  type ExtensionReview,
  reviewNeedsApproval,
  reviewsNeedingApproval,
  type UpgradeJournal,
} from './maintenance';

const journal = (reviews: ExtensionReview[] | null): UpgradeJournal => ({
  from_version: '1.2.0',
  id: 'offline-1',
  revision: 1,
  schema: 1,
  staged_binary: '/updates/candidate',
  state: 'rehearsed',
  to_version: '1.3.0',
  updated_at: '2026-10-26T12:00:00Z',
  extension_reviews: reviews,
});

test('a new request or an older version needs approval', () => {
  expect(
    reviewNeedsApproval({ module: 'a', version: '1', new_asks: ['x'] }),
  ).toBe(true);
  expect(
    reviewNeedsApproval({ module: 'a', version: '1', downgrade: true }),
  ).toBe(true);
  expect(
    reviewNeedsApproval({
      module: 'a',
      version: '1',
      asks: ['x'],
      new_asks: [],
    }),
  ).toBe(false);
  expect(
    reviewNeedsApproval({ module: 'a', version: '1', new_asks: null }),
  ).toBe(false);
});

test('only the reviews that need approval are listed', () => {
  expect(reviewsNeedingApproval(undefined)).toEqual([]);
  expect(reviewsNeedingApproval(journal(null))).toEqual([]);
  expect(
    reviewsNeedingApproval(
      journal([
        { module: 'same', version: '2', asks: ['x'] },
        { module: 'older', version: '1', downgrade: true },
      ]),
    ).map((review) => review.module),
  ).toEqual(['older']);
});
