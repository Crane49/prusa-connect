// What the page shows, as the address bar spells it, so a reload, a link and
// the back button land on the same tab with the same filters.
//
// The page has one route of its own, `/m/<namespace>/buddy-debug-logs`, so the
// state rides in the query: `?tab=metrics&mprinter=device:7&metric=temp_bed`.
// A value equal to its default is left out, so the bare address is the page
// as it first opens. Plain functions, so a test reads them without a browser.
import { DEFAULT_FILTER, type LogFilter } from './logs.svelte';
import {
  ALL,
  FILTER_LEVELS,
  type FilterLevel,
  type Origin,
  originOf,
  originValue,
  WINDOWS,
  type Window,
} from './presentation';

export const TABS = ['logs', 'metrics', 'sources'] as const;
export type Tab = (typeof TABS)[number];

/** The metrics tab's choices. */
export interface MetricsChoice {
  origin: Origin;
  window: Window;
  /** The metric drawn, or undefined for the first one that arrived. */
  name: string | undefined;
  /** The field drawn, or undefined for the metric's first one. */
  field: string | undefined;
}

export interface PageState {
  tab: Tab;
  logs: LogFilter;
  metrics: MetricsChoice;
  /** Whether the ports' counts are unfolded. */
  status: boolean;
  /** Whether the steps on the printer are unfolded. */
  guide: boolean;
}

export const DEFAULT_METRICS: MetricsChoice = {
  origin: ALL,
  window: 1,
  name: undefined,
  field: undefined,
};

export const DEFAULT_STATE: PageState = {
  tab: 'logs',
  logs: DEFAULT_FILTER,
  metrics: DEFAULT_METRICS,
  status: false,
  guide: false,
};

const OPEN = 'open';

const windowOf = (text: string | null, fallback: Window): Window => {
  const hours = Number(text);

  return (WINDOWS as readonly number[]).includes(hours)
    ? (hours as Window)
    : fallback;
};

/** A query value, or undefined for an absent or empty one. */
const given = (text: string | null): string | undefined =>
  text === null || text === '' ? undefined : text;

const levelOf = (text: string | null): FilterLevel | undefined =>
  (FILTER_LEVELS as readonly string[]).includes(text ?? '')
    ? (text as FilterLevel)
    : undefined;

/** The state a query names. An unknown or malformed value is its default. */
export const stateOf = (search: string): PageState => {
  const query = new URLSearchParams(search);
  const tab = query.get('tab') ?? '';

  return {
    tab: (TABS as readonly string[]).includes(tab)
      ? (tab as Tab)
      : DEFAULT_STATE.tab,
    logs: {
      origin: originOf(query.get('printer') ?? ''),
      level: levelOf(query.get('level')),
      component: query.get('component') ?? '',
      text: query.get('q') ?? '',
      window: windowOf(query.get('hours'), DEFAULT_FILTER.window),
    },
    metrics: {
      origin: originOf(query.get('mprinter') ?? ''),
      window: windowOf(query.get('mhours'), DEFAULT_METRICS.window),
      name: given(query.get('metric')),
      field: given(query.get('field')),
    },
    status: query.get('status') === OPEN,
    guide: query.get('guide') === OPEN,
  };
};

/** The query for a state, with every default left out, starting with `?` when not empty. */
export const searchOf = (state: PageState): string => {
  const query = new URLSearchParams();
  const put = (key: string, value: string, fallback: string): void => {
    if (value !== fallback) query.set(key, value);
  };

  put('tab', state.tab, DEFAULT_STATE.tab);
  put('printer', originValue(state.logs.origin), originValue(ALL));
  put('level', state.logs.level ?? '', '');
  put('component', state.logs.component.trim(), '');
  put('q', state.logs.text.trim(), '');
  put('hours', String(state.logs.window), String(DEFAULT_FILTER.window));
  put('mprinter', originValue(state.metrics.origin), originValue(ALL));
  put('mhours', String(state.metrics.window), String(DEFAULT_METRICS.window));
  put('metric', state.metrics.name ?? '', '');
  put('field', state.metrics.field ?? '', '');
  put('status', state.status ? OPEN : '', '');
  put('guide', state.guide ? OPEN : '', '');

  const text = query.toString();

  return text === '' ? '' : `?${text}`;
};
