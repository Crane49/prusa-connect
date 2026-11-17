// The one request that reads what each event type this build publishes means.
// A service names the SDK operation and its parameters and holds no state
// (svelte-data.md).
import type { PrusaConnect } from '@prusa/connect';

/** What the event-type vocabulary needs of the SDK, and no more. */
export type EventTypesClient = Pick<PrusaConnect, 'getEventTypeVocabulary'>;

/** The answer `GET /api/v1/vocabularies/event-types` gives. */
export type EventTypesAnswer = Awaited<
  ReturnType<PrusaConnect['getEventTypeVocabulary']>
>;

/** One event type: its class, its one-line summary and the long form. */
export type EventTypeEntry = NonNullable<EventTypesAnswer['items']>[number];

/**
 * What this build publishes. The list belongs to the server rather than to
 * the client, so a server carrying a module this build never heard of still
 * names its events.
 */
export const readEventTypes = (
  client: EventTypesClient,
): Promise<EventTypesAnswer> => client.getEventTypeVocabulary();
