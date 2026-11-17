import { describe, expect, it } from 'vitest';

import fragment from '../../openapi.json';
import { jobFailures, operations } from './api-reference';
import czech from './locales/cs.json';
import english from './locales/en.json';

const texts: Record<string, Record<string, string>> = {
  en: english,
  cs: czech,
};

describe('the API tab', () => {
  it('lists the scope of every operation the fragment describes', () => {
    const described = Object.entries(fragment.paths).flatMap(
      ([path, methods]) =>
        Object.keys(methods).map((method) => `${method.toUpperCase()} ${path}`),
    );
    const listed = operations().map((op) => `${op.method} ${op.path}`);

    expect(listed.sort()).toEqual(described.sort());
    expect(operations().find((op) => op.id === 'install-slicer')?.scope).toBe(
      'administer',
    );
    expect(operations().find((op) => op.id === 'upload-job')?.scope).toBe(
      'operate',
    );
  });

  it('has a text in every language for every way a job fails', () => {
    const codes = jobFailures();

    expect(codes).toContain('server-slicing/NOT_PRUSASLICER');
    for (const [language, catalogue] of Object.entries(texts)) {
      for (const code of codes) {
        expect(catalogue[`error.${code}`], `${language} ${code}`).toBeTruthy();
      }
    }
  });
});
