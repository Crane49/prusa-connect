// The requests the Fractal page makes. A service names the SDK operation and
// its parameters and keeps no state (svelte-data.md).
//
// Fractal is the link between two Prusa Connect servers. A link instance says
// which role this server has, how a node joins, and which nodes have joined.
import type { PrusaConnect, EventType } from '@prusa/connect';

/** Every link this server declares, as `list-links` answers. */
export type LinkPage = Awaited<ReturnType<PrusaConnect['listLinks']>>;

/** One link instance: its mode, its enrolment, its key and its nodes. */
export type LinkInstance = NonNullable<LinkPage['items']>[number];

/** One other server on the link, with `alive`, `latency_ms` and `admission`. */
export type LinkNode = NonNullable<LinkInstance['nodes']>[number];

/** What the link store needs of the SDK, and no more. */
export type LinkClient = Pick<
  PrusaConnect,
  'listLinks' | 'getLink' | 'approveLinkNode'
>;

/** The one event that moves a link. It arrives for every state change. */
export const LINK_EVENTS: readonly EventType[] = [
  'com.prusa.connect.link.state.changed',
];

/** A node this server has admitted, against one still waiting for a decision. */
export const NODE_PENDING = 'pending';

/** Every link instance this caller may see. */
export const readLinks = (client: LinkClient): Promise<LinkPage> =>
  client.listLinks();

/** One link instance by the name it was declared under. */
export const readLink = (
  client: LinkClient,
  name: string,
): Promise<LinkInstance> => client.getLink({ path: { name } });

/**
 * Admits one waiting node. The answer is the whole link instance again, so
 * the store replaces its row from the answer instead of reading the list.
 */
export const approveNode = (
  client: LinkClient,
  name: string,
  nodeId: string,
): Promise<LinkInstance> =>
  client.approveLinkNode({ path: { name, id: nodeId } });
