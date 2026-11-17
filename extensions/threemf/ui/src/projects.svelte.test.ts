// The store: one flight per file, a refusal kept with its code, and a read
// that was stopped records nothing.
import { describe, expect, it } from 'vitest';

import { Projects } from './projects.svelte';
import { ANSWERS, fakeService } from './test/projects';

describe('Projects', () => {
  it('asks once for a file two callers want', async () => {
    const service = fakeService(ANSWERS);
    const projects = new Projects(service);

    await Promise.all([
      projects.describe('local', 'projects/multibed.3mf'),
      projects.describe('local', 'projects/multibed.3mf'),
    ]);
    await projects.describe('local', 'projects/multibed.3mf');

    expect(service.asked).toEqual(['local:projects/multibed.3mf']);
    expect(
      projects.described('local', 'projects/multibed.3mf')?.project,
    ).toBeDefined();
  });

  it('keeps a refusal with its code', async () => {
    const projects = new Projects(fakeService(ANSWERS));

    await projects.describe('local', 'projects/damaged.3mf');

    expect(projects.described('local', 'projects/damaged.3mf')?.fault).toEqual(
      expect.objectContaining({ code: 'threemf/NOT_A_PROJECT', status: 422 }),
    );
  });

  it('tells a file not asked about from one refused', () => {
    const projects = new Projects(fakeService(ANSWERS));

    expect(projects.described('local', 'projects/multibed.3mf')).toBe(
      undefined,
    );
  });

  it('records nothing for a read that was stopped', async () => {
    const projects = new Projects(fakeService(ANSWERS));
    const reading = new AbortController();
    reading.abort();

    await projects.describe('local', 'projects/multibed.3mf', reading.signal);

    expect(projects.described('local', 'projects/multibed.3mf')).toBe(
      undefined,
    );
  });

  it('keeps one mesh per object of one file', async () => {
    const projects = new Projects(fakeService(ANSWERS));

    await projects.describeMesh('local', 'projects/multibed.3mf', 1);

    expect(
      projects.describedMesh('local', 'projects/multibed.3mf', 1)?.mesh,
    ).toBeDefined();
    expect(
      projects.describedMesh('local', 'projects/multibed.3mf', 0),
    ).toBeUndefined();
  });
});
