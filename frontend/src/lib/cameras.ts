// How a page keeps a camera ticking, and stops it when nobody is looking.
//
// The store asks for a new still frame on its own clock. A tab the person has
// switched away from is a tab drawing nothing, so the tick stops there: the
// frames would be thrown away, and on a fleet of four hundred each tick is
// four hundred requests against a Raspberry Pi.
import type { Cameras } from '@prusa/connect-ui/data';

/**
 * Starts the tick and stops it while this tab is hidden. It answers the
 * function that stops it for good, which a screen returns from its effect.
 *
 * `floorMs` is the fastest this page will ask, whatever a camera declares it
 * can do. The fleet and one printer's page ask at different rates, so each
 * passes its own.
 */
export const watchCameras = (
  cameras: Cameras,
  floorMs: number,
): (() => void) => {
  const hidden = (): boolean => document.visibilityState === 'hidden';

  const follow = (): void => {
    if (hidden()) {
      cameras.stop();

      return;
    }

    // A frame asked for now, so a tab that comes back shows what the camera
    // has rather than the frame it was left with.
    cameras.refresh();
    cameras.listen(floorMs);
  };

  if (!hidden()) cameras.listen(floorMs);
  document.addEventListener('visibilitychange', follow);

  return () => {
    document.removeEventListener('visibilitychange', follow);
    cameras.stop();
  };
};
