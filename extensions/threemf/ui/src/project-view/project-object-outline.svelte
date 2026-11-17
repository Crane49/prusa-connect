<!--
  One copy of an object seen from above, alone and as large as the box
  allows, in the colour of its filament, with its width and depth written
  along its edges.

  It is the outline the server sends for the plan, so it shows the footprint
  and not the shape: a person reads the size and the turn of the copy here,
  and the shape in the 3D view or the slicer's picture.
-->
<script lang="ts">
  import { millimetres } from './labels';
  import type { Point, ProjectInstance } from './model';

  interface Props {
    instance: ProjectInstance;
    /** The colour of the filament, as the file writes it. */
    colour?: string | undefined;
    /** What the picture shows, for a screen reader. */
    label: string;
  }

  let { instance, colour, label }: Props = $props();

  const outline = $derived(
    instance.outline.length > 0
      ? instance.outline
      : [
          instance.min,
          { x: instance.min.x + instance.size.x, y: instance.min.y },
          {
            x: instance.min.x + instance.size.x,
            y: instance.min.y + instance.size.y,
          },
          { x: instance.min.x, y: instance.min.y + instance.size.y },
        ],
  );

  // The drawing in millimetres with y pointing down, framed by a margin of a
  // fifth of the larger side for the two measures.
  const view = $derived.by(() => {
    const xs = outline.map((point: Point) => point.x);
    const ys = outline.map((point: Point) => -point.y);
    const minX = Math.min(...xs);
    const maxX = Math.max(...xs);
    const minY = Math.min(...ys);
    const maxY = Math.max(...ys);
    const side = Math.max(maxX - minX, maxY - minY, 1);
    const margin = side * 0.2;

    return {
      minX,
      maxX,
      minY,
      maxY,
      margin,
      text: side * 0.07,
      box: `${String(minX - margin)} ${String(minY - margin)} ${String(maxX - minX + 2 * margin)} ${String(maxY - minY + 2 * margin)}`,
      points: outline
        .map((point: Point) => `${String(point.x)},${String(-point.y)}`)
        .join(' '),
    };
  });
</script>

<svg
  class="outline-view block size-full"
  viewBox={view.box}
  role="img"
  aria-label={label}
  style={colour === undefined ? undefined : `--copy: ${colour}`}
  data-slot="project-object-outline"
>
  <polygon class="shape" points={view.points} />
  <line
    class="measure"
    x1={view.minX}
    x2={view.maxX}
    y1={view.maxY + view.margin * 0.45}
    y2={view.maxY + view.margin * 0.45}
  />
  <text
    class="fill-muted-foreground font-sans"
    x={(view.minX + view.maxX) / 2}
    y={view.maxY + view.margin * 0.8}
    font-size={view.text}
    text-anchor="middle"
    dominant-baseline="central"
  >
    {millimetres(instance.size.x)}
  </text>
  <line
    class="measure"
    x1={view.maxX + view.margin * 0.45}
    x2={view.maxX + view.margin * 0.45}
    y1={view.minY}
    y2={view.maxY}
  />
  <text
    class="fill-muted-foreground font-sans"
    x={view.maxX + view.margin * 0.8}
    y={(view.minY + view.maxY) / 2}
    font-size={view.text}
    text-anchor="middle"
    dominant-baseline="central"
    transform="rotate(90 {view.maxX + view.margin * 0.8} {(view.minY +
      view.maxY) /
      2})"
  >
    {millimetres(instance.size.y)}
  </text>
</svg>

<style>
  .outline-view {
    --copy-ink: var(--copy, var(--muted-foreground));
  }

  .shape {
    fill: color-mix(in oklch, var(--copy-ink) 70%, transparent);
    stroke: color-mix(in oklch, var(--copy-ink), var(--foreground) 40%);
    stroke-width: 1.5;
    vector-effect: non-scaling-stroke;
  }

  .measure {
    stroke: var(--muted-foreground);
    stroke-width: 1;
    vector-effect: non-scaling-stroke;
  }
</style>
