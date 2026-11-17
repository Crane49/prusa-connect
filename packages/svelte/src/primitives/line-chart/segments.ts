import type { TelemetryPoint } from '../../domain';

/**
 * Splits a line into the runs that are drawn joined. A point whose value is
 * `NaN` is a gap: it is dropped and the line breaks there. The line also
 * breaks between two neighbours that lie on either side of a break instant,
 * and a break that falls on a point starts the next run at that point.
 */
export const segmentsOf = (
  points: readonly TelemetryPoint[],
  breaks: readonly number[],
): TelemetryPoint[][] => {
  const runs: TelemetryPoint[][] = [];
  let run: TelemetryPoint[] = [];
  let previous: TelemetryPoint | undefined;

  const close = (): void => {
    if (run.length > 0) runs.push(run);
    run = [];
  };

  for (const point of points) {
    if (Number.isNaN(point.value)) {
      close();
      previous = undefined;
      continue;
    }
    if (
      previous !== undefined &&
      breaks.some(
        (at) => previous !== undefined && previous.at < at && at <= point.at,
      )
    ) {
      close();
    }
    run.push(point);
    previous = point;
  }
  close();

  return runs;
};
