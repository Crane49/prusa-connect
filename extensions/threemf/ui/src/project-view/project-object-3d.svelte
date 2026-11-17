<!--
  One object of a 3MF project in 3D, each part in the colour of its filament.
  Dragging turns it, the wheel or a pinch brings it nearer, and the arrow
  keys, + and - and 0 do the same from the keyboard.

  The WebGL code is imported when the first object is drawn, so a page that
  never opens an object never loads it. Where the browser has no WebGL, the
  component calls `onunavailable` and the panel shows the object from above.
-->
<script lang="ts">
  import RotateCcwIcon from '@lucide/svelte/icons/rotate-ccw';

  import type { ProjectMesh } from '../api';
  import { t } from '../i18n';
  import { Button } from '@prusa/connect-ui/primitives';
  import type { ProjectFilament } from './model';
  import type { Rgb, Viewer } from './mesh-viewer';

  interface Props {
    mesh: ProjectMesh;
    /** The filaments of the setup, in extruder order, which colour the parts. */
    filaments: readonly ProjectFilament[];
    /** What the picture shows, for a screen reader. */
    label: string;
    /** Called when the browser cannot draw in 3D. */
    onunavailable?: (() => void) | undefined;
  }

  let { mesh, filaments, label, onunavailable }: Props = $props();

  let viewer = $state<Viewer | undefined>(undefined);

  const STEP = 0.15;
  const ZOOM = 1.2;

  // Starts the viewer on the canvas once its code is in, and lets go of it
  // when the canvas leaves or the mesh changes. This reaches the DOM and an
  // external renderer, which is what an attachment is for.
  const draw = (canvas: HTMLCanvasElement) => {
    let made: Viewer | undefined;
    let gone = false;
    const observer = new ResizeObserver(() => made?.resize());

    void import('./mesh-viewer').then(({ createViewer, liftDark, rgbOf }) => {
      if (gone) return;
      const fallback: Rgb = rgbOf(
        getComputedStyle(canvas).getPropertyValue('--muted-foreground'),
      ) ?? [0.6, 0.6, 0.6];
      made = createViewer(canvas, {
        vertices: mesh.vertices,
        triangles: mesh.triangles,
        parts: mesh.parts.map((part) => {
          const colour = filaments[(part.extruder ?? 1) - 1]?.colour;

          return {
            triangles: part.triangles,
            colour: liftDark(
              colour === undefined ? fallback : (rgbOf(colour) ?? fallback),
            ),
          };
        }),
      });
      if (made === undefined) {
        onunavailable?.();

        return;
      }
      viewer = made;
      observer.observe(canvas);
    });

    return () => {
      gone = true;
      observer.disconnect();
      made?.destroy();
      viewer = undefined;
    };
  };

  const onKey = (event: KeyboardEvent): void => {
    const moves: Record<string, () => void> = {
      ArrowLeft: () => viewer?.turn(STEP, 0),
      ArrowRight: () => viewer?.turn(-STEP, 0),
      ArrowUp: () => viewer?.turn(0, STEP),
      ArrowDown: () => viewer?.turn(0, -STEP),
      '+': () => viewer?.zoom(ZOOM),
      '=': () => viewer?.zoom(ZOOM),
      '-': () => viewer?.zoom(1 / ZOOM),
      '0': () => viewer?.reset(),
      Home: () => viewer?.reset(),
    };
    const move = moves[event.key];
    if (move === undefined) return;
    event.preventDefault();
    move();
  };
</script>

<div class="relative size-full" data-slot="project-object-3d">
  <!-- The keys go to a box around the canvas: a canvas the keyboard reaches
       is read as a control, and a control cannot be named as a view. The
       application role hands the arrow keys to it, as a tile of the
       dashboard takes them. -->
  <!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions -->
  <div
    class="size-full rounded-sm bg-surface outline-none focus-visible:ring-2 focus-visible:ring-ring"
    role="application"
    aria-roledescription={t('projectView.panel.viewer')}
    aria-label={label}
    tabindex="0"
    onkeydown={onKey}
  >
    {#key mesh}
      <canvas
        class="block size-full cursor-grab touch-none active:cursor-grabbing"
        aria-hidden="true"
        {@attach draw}
      ></canvas>
    {/key}
  </div>
  {#if viewer !== undefined}
    <Button
      variant="ghost"
      size="icon-sm"
      class="absolute end-1 inset-bs-1"
      label={t('projectView.panel.resetView')}
      onclick={() => viewer?.reset()}
    >
      <RotateCcwIcon aria-hidden="true" />
    </Button>
  {/if}
</div>
