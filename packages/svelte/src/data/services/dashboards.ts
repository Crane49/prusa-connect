// The requests the dashboard screen makes (ADR-20261008-sww rules 1 to 11).
import type { PrusaConnect } from '@prusa/connect';

/** Every dashboard the caller sees, as `list-dashboards` answers. */
export type DashboardPage = Awaited<ReturnType<PrusaConnect['listDashboards']>>;

/** One dashboard as the list shows it. */
export type DashboardRow = DashboardPage['items'][number];

/** One dashboard with every card on it. */
export type Dashboard = Awaited<ReturnType<PrusaConnect['getDashboard']>>;

/** What `create-dashboard` takes. */
export type DashboardBody = Parameters<
  PrusaConnect['createDashboard']
>[0]['body'];

/** What `replace-dashboard` takes: a body and the revision it was made against. */
export type DashboardReplacement = Parameters<
  PrusaConnect['replaceDashboard']
>[0]['body'];

/** A draft as the screen holds it, with its lists read-only. */
export interface DashboardDraftBody {
  readonly name: string;
  readonly visibility: NonNullable<DashboardBody['visibility']>;
  readonly columns: number;
  readonly density: NonNullable<DashboardBody['density']>;
  readonly cards: readonly NonNullable<DashboardBody['cards']>[number][];
}

/** The body of a write, copied out of the draft so nothing shares its arrays. */
export const bodyOf = (draft: DashboardDraftBody): DashboardBody => ({
  name: draft.name,
  visibility: draft.visibility,
  columns: draft.columns,
  density: draft.density,
  cards: draft.cards.map((card) => ({ ...card, config: { ...card.config } })),
});

/** What the dashboards store needs of the SDK. */
export type DashboardsClient = Pick<
  PrusaConnect,
  | 'listDashboards'
  | 'getDashboard'
  | 'createDashboard'
  | 'replaceDashboard'
  | 'deleteDashboard'
  | 'setDefaultDashboard'
>;

export const readDashboards = (
  client: DashboardsClient,
  signal?: AbortSignal,
): Promise<DashboardPage> => client.listDashboards({}, signal);

export const readDashboard = (
  client: DashboardsClient,
  id: number,
  signal?: AbortSignal,
): Promise<Dashboard> =>
  client.getDashboard({ path: { dashboard: String(id) } }, signal);

export const createDashboard = (
  client: DashboardsClient,
  draft: DashboardDraftBody,
): Promise<Dashboard> => client.createDashboard({ body: bodyOf(draft) });

/** Replaces one dashboard. The body carries the revision it was made against. */
export const replaceDashboard = (
  client: DashboardsClient,
  id: number,
  draft: DashboardDraftBody & { readonly revision: number },
): Promise<Dashboard> =>
  client.replaceDashboard({
    path: { dashboard: String(id) },
    body: { ...bodyOf(draft), revision: draft.revision },
  });

export const deleteDashboard = async (
  client: DashboardsClient,
  id: number,
): Promise<void> => {
  await client.deleteDashboard({ path: { dashboard: String(id) } });
};

/** Marks one dashboard the default, or none. Answers the default now. */
export const setDefaultDashboard = async (
  client: DashboardsClient,
  id: number | null,
): Promise<number | null> =>
  (await client.setDefaultDashboard({ body: { id } })).default_id;
