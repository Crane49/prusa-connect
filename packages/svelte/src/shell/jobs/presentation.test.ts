import { describe, expect, it } from 'vitest';

import { jobsRead } from './fixtures';
import {
  compareJobs,
  DEFAULT_SORT,
  filterJobs,
  jobFacetCounts,
  jobRowsOf,
  nextSort,
  NO_JOBS_SELECTED,
  searchTerm,
} from './presentation';

const rows = jobRowsOf(jobsRead());

const onlyCore = { ...NO_JOBS_SELECTED, printer: ['prn-core'] };

describe('the prints a source carries', () => {
  it('flattens every printer that answered onto one list', () => {
    expect(rows).toHaveLength(12);
    expect(rows.every((row) => row.printerName !== '')).toBe(true);
  });

  it('gives a print still on the bed no span, and a finished one its own', () => {
    const running = rows.find((row) => row.running);
    const finished = rows.find((row) => row.id === 'prn-rack-3:41');

    expect(running?.durationSeconds).toBeUndefined();
    expect(finished?.durationSeconds).toBe(128 * 60);
  });
});

describe('what a chip would bring', () => {
  it('counts with its own dimension left out', () => {
    // Three prints ran on CORE One, and picking it must not reduce the
    // printer chips beside it to that one printer.
    const counts = jobFacetCounts(rows, onlyCore, '', 'printer');

    expect(counts.get('prn-core')).toBe(3);
    expect(counts.get('prn-mini')).toBe(2);
  });

  it('counts within the other dimension, and within the search', () => {
    const states = jobFacetCounts(rows, onlyCore, '', 'state');
    expect(states.get('FINISHED')).toBe(1);

    const searched = jobFacetCounts(
      rows,
      NO_JOBS_SELECTED,
      searchTerm(' Benchy '),
      'printer',
    );
    expect(searched.get('prn-mini')).toBe(1);
    expect(searched.get('prn-rack-3')).toBeUndefined();
  });

  it('reads zero for a printer the filters leave nothing on', () => {
    const counts = jobFacetCounts(
      rows,
      NO_JOBS_SELECTED,
      searchTerm('benchy'),
      'printer',
      ['prn-xl'],
    );

    expect(counts.get('prn-xl')).toBe(0);
  });
});

describe('narrowing and ordering', () => {
  it('applies both dimensions and the search at once', () => {
    const found = filterJobs(rows, onlyCore, searchTerm('benchy'));

    expect(found.map((row) => row.id)).toEqual(['prn-core:16']);
  });

  it('opens on the newest print and reverses on a second press', () => {
    const newest = [...rows].sort((a, b) => compareJobs(a, b, DEFAULT_SORT));
    expect(newest[0]?.id).toBe('prn-rack-3:42');

    const reversed = nextSort(DEFAULT_SORT, 'started');
    expect(reversed.ascending).toBe(true);
    expect([...rows].sort((a, b) => compareJobs(a, b, reversed))[0]?.id).toBe(
      'prn-mk3s:89',
    );
  });

  it('hands the order to the other column, longest first', () => {
    const byDuration = nextSort(DEFAULT_SORT, 'duration');

    expect(byDuration).toEqual({ column: 'duration', ascending: false });
    expect([...rows].sort((a, b) => compareJobs(a, b, byDuration))[0]?.id).toBe(
      'prn-rack-3:40',
    );
  });
});
