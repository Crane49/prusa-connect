// A stopwatch for the browser tests, counted in animation frames.
//
// A dialog has to be on the screen on the frame after the press that opened
// it. Wall-clock milliseconds cannot say that: a test machine under load draws
// a slow frame and the number moves, while the thing under test, how many
// frames the panel waited, does not. So the count is frames, and the press it
// counts from is the real `pointerdown` the driver sends.

/** What the watched element looked like on the frame it first had a box. */
export interface FirstFrame {
  /** Frames between the press and the element being on the screen. */
  frames: number;
  /** Its opacity on that frame. A panel part way through a fade is not shown. */
  opacity: number;
  /** Whether an animation was still running on it. */
  animating: boolean;
}

/** Gives up rather than hanging, on a press that opens nothing. */
const LIMIT = 60;

/**
 * Watches `selector` from the next press, and answers on the first frame the
 * element has a box on the screen. Call it before the click and await it
 * after, so the arming happens first.
 */
export const onNextFrame = (selector: string): Promise<FirstFrame> =>
  new Promise((resolve) => {
    const look = (): void => {
      let frames = 0;

      const tick = (): void => {
        const found = document.querySelector(selector);
        if (found instanceof HTMLElement) {
          const box = found.getBoundingClientRect();
          if (box.width > 0 && box.height > 0) {
            resolve({
              frames,
              opacity: Number(getComputedStyle(found).opacity),
              animating: found
                .getAnimations()
                .some((one) => one.playState === 'running'),
            });

            return;
          }
        }

        frames += 1;
        if (frames > LIMIT) {
          resolve({ frames, opacity: 0, animating: false });

          return;
        }
        requestAnimationFrame(tick);
      };

      requestAnimationFrame(tick);
    };

    window.addEventListener('pointerdown', look, {
      capture: true,
      once: true,
    });
  });
