// One transfers store per printer, shared by every page that shows that
// printer's transfers. A plain module and not a rune file, because the map
// only holds the stores and is never drawn.
import { Transfers, type TransfersClient } from '@prusa/connect-ui/data';

/**
 * Answers the one store for a printer, and builds it the first time it is
 * asked.
 */
export interface TransferStores {
  (deviceId: string): Transfers;
  /**
   * Resets every store built so far and forgets them. The app calls it when
   * the person signed in changes, so the next person never sees the
   * transfers of the last one. A page still holding an old store sees it
   * empty.
   */
  reset(): void;
}

/**
 * `principal` answers who is signed in when a store is built. The store
 * passes it to the event stream, so the stream does not carry on for a
 * different person after a drop.
 */
export const transferStores = (
  client: TransfersClient,
  principal: () => string | undefined = () => undefined,
): TransferStores => {
  const held = new Map<string, Transfers>();

  const find = (deviceId: string): Transfers => {
    const found = held.get(deviceId);
    if (found !== undefined) return found;

    const person = principal();
    const built = new Transfers(
      client,
      deviceId,
      person === undefined ? {} : { principal: person },
    );
    held.set(deviceId, built);

    return built;
  };

  const reset = (): void => {
    for (const store of held.values()) store.reset();
    held.clear();
  };

  return Object.assign(find, { reset });
};
