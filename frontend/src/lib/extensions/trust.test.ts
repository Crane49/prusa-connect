import { describe, expect, it } from 'vitest';

import { needsConsent, trustNoticeOf } from './trust';

describe('the trust of an install', () => {
  it('says nothing about an official extension', () => {
    const notice = trustNoticeOf([
      { title: 'Spool tracker', author: 'Prusa Research', trust: 'official' },
    ]);

    expect(notice).toEqual({ unverified: [], authors: [], trusted: [] });
    expect(needsConsent(notice)).toBe(false);
  });

  it('names a trusted extension and asks for no consent', () => {
    const notice = trustNoticeOf([
      { title: 'Parts', author: 'Acme', trust: 'verified' },
    ]);

    expect(notice.trusted).toEqual(['Parts']);
    expect(needsConsent(notice)).toBe(false);
  });

  it('asks for consent for an unverified extension, with who publishes it', () => {
    const notice = trustNoticeOf([
      { title: 'Nozzle watch', author: 'Jane', trust: 'community' },
    ]);

    expect(notice.unverified).toEqual(['Nozzle watch']);
    expect(notice.authors).toEqual(['Jane']);
    expect(needsConsent(notice)).toBe(true);
  });

  // An official extension that requires an unverified one brings it in with
  // the same approval, so the person must see it.
  it('counts the extensions the plan adds with the one picked', () => {
    const notice = trustNoticeOf([
      { title: 'Spool tracker', author: 'Prusa Research', trust: 'official' },
      { title: 'Reader', author: '', trust: 'community' },
      { title: 'Parts', author: 'Acme', trust: 'verified' },
    ]);

    expect(notice.unverified).toEqual(['Reader']);
    expect(notice.authors).toEqual([]);
    expect(notice.trusted).toEqual(['Parts']);
    expect(needsConsent(notice)).toBe(true);
  });

  it('says nothing about a release no marketplace marks', () => {
    const notice = trustNoticeOf([{ title: 'Local build' }]);

    expect(needsConsent(notice)).toBe(false);
    expect(notice.trusted).toEqual([]);
  });
});
