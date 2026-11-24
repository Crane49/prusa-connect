import { ApiError } from '@prusa/connect';
import { describe, expect, it } from 'vitest';

import { said } from './api';

describe('said', () => {
  it("reads the sentence of the module's own refusal", () => {
    const refused = new ApiError(422, '{"code":"REFUSED","message":"the camera refused it with code 102"}');

    expect(refused.message).toBe('HTTP 422');
    expect(said(refused)).toBe('the camera refused it with code 102');
  });

  it('leaves an error with no sentence of its own alone', () => {
    expect(said(new ApiError(502, 'Bad Gateway'))).toBeUndefined();
    expect(said(new Error('offline'))).toBeUndefined();
  });
});
