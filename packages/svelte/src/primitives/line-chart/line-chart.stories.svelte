<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import type { TelemetryPoint } from '../../domain';
  import LineChart from './line-chart.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/LineChart',
    component: LineChart,
  });

  const start = Date.parse('2026-09-23T09:00:00.000Z');

  const wave = (
    base: number,
    amplitude: number,
    period: number,
  ): TelemetryPoint[] =>
    Array.from({ length: 120 }, (_, index) => ({
      at: start + index * 60_000,
      value: base + amplitude * Math.sin((index / period) * Math.PI * 2),
    }));

  const nozzle = wave(215, 4, 15);
  const bed = wave(60, 1.5, 15);

  // A heat-up ramp, then a bed held at 60 that the sensor reads within 0.4.
  const noisyBed: TelemetryPoint[] = Array.from(
    { length: 120 },
    (_, index) => ({
      at: start + index * 60_000,
      value:
        index < 10
          ? 22 + (index * 38) / 10
          : 60 + 0.4 * Math.sin(index * 12.9898) * Math.cos(index * 4.1414),
    }),
  );

  // Sixty readings one second apart, climbing towards a budget of 128.
  const hour: TelemetryPoint[] = Array.from({ length: 60 }, (_, index) => ({
    at: start + index * 1000,
    value: 60 + index + 6 * Math.sin(index / 3),
  }));

  const gapped: TelemetryPoint[] = hour.map((point, index) =>
    index >= 44 && index < 50 ? { ...point, value: Number.NaN } : point,
  );
</script>

<!-- No React source: this is the library's first chart of any kind. -->

<Story name="One Series" asChild>
  <LineChart
    label="Nozzle temperature over the last two hours"
    unit="°C"
    series={[{ id: 'nozzle', label: 'Nozzle', points: nozzle }]}
  />
</Story>

<Story name="Two Series" asChild>
  <LineChart
    label="Nozzle and bed temperature over the last two hours"
    unit="°C"
    series={[
      { id: 'nozzle', label: 'Nozzle', points: nozzle },
      { id: 'bed', label: 'Bed', points: bed },
    ]}
  />
</Story>

<Story name="One Reading" asChild>
  <LineChart
    label="Nozzle temperature, one reading so far"
    unit="°C"
    series={[{ id: 'nozzle', label: 'Nozzle', points: nozzle.slice(0, 1) }]}
  />
</Story>

<Story name="Empty" asChild>
  <LineChart
    label="Nozzle temperature, nothing reported yet"
    unit="°C"
    series={[{ id: 'nozzle', label: 'Nozzle', points: [] }]}
  />
</Story>

<Story name="Noisy Reading" asChild>
  <LineChart
    label="Bed temperature, steady with sensor noise"
    unit="°C"
    minStep={5}
    series={[{ id: 'bed', label: 'Bed', points: noisyBed }]}
  />
</Story>

<Story name="Threshold" asChild>
  <LineChart
    label="Module memory against its budget"
    unit="MB"
    threshold={{ value: 128, label: 'Memory budget' }}
    series={[{ id: 'memory', label: 'Memory', points: hour }]}
  />
</Story>

<Story name="Markers" asChild>
  <LineChart
    label="Module memory with its restarts"
    unit="MB"
    markers={[{ at: start + 20_000, label: 'Restart' }]}
    series={[{ id: 'memory', label: 'Memory', points: hour }]}
  />
</Story>

<Story name="Regions" asChild>
  <LineChart
    label="Module memory with the time it was stopped"
    unit="MB"
    regions={[{ from: start + 30_000, to: start + 40_000, label: 'Stopped' }]}
    series={[{ id: 'memory', label: 'Memory', points: hour }]}
  />
</Story>

<Story name="Gaps" asChild>
  <LineChart
    label="Module memory with missing readings"
    unit="MB"
    series={[{ id: 'memory', label: 'Memory', points: gapped }]}
  />
</Story>

<Story name="Memory Hour" asChild>
  <LineChart
    label="Module memory over the last minute"
    unit="MB"
    threshold={{ value: 128, label: 'Memory budget' }}
    markers={[{ at: start + 20_000, label: 'Restart' }]}
    regions={[{ from: start + 30_000, to: start + 40_000, label: 'Stopped' }]}
    series={[{ id: 'memory', label: 'Memory', points: gapped }]}
  />
</Story>
