import { describe, expect, it } from 'vitest';

import { subjectInitials } from './initials';

describe('subjectInitials', () => {
  it('drops the kind and takes the first letter of up to two parts', () => {
    expect(subjectInitials('user:alice')).toBe('AL');
  });

  it('is empty for an empty subject', () => {
    expect(subjectInitials('')).toBe('');
  });

  it('splits on the same separators without a kind to drop', () => {
    expect(subjectInitials('alice.smith')).toBe('AS');
  });

  it('takes the first two letters of a single part', () => {
    expect(subjectInitials('user:admin')).toBe('AD');
  });

  it('ignores every part past the second', () => {
    expect(subjectInitials('user:alice.smith.jones')).toBe('AS');
  });
});
