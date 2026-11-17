<script lang="ts">
  import { onMount } from 'svelte';

  import {
    LIFECYCLE_TRANSITIONS,
    projectCommand,
    STATE_PATH,
    textAt,
    type CommandProjection,
  } from '../../../domain';
  import { describeFault, t } from '../../../i18n';
  import { FaultAlert, Skeleton } from '../../../primitives';
  import CommandCatalogue from './command-catalogue.svelte';
  import GcodeConsole from './gcode-console.svelte';
  import { verbsOf, type PrinterCommandsPanelProps } from './types';
  import { isOfferedHere } from './vocabulary';

  let {
    commands,
    device,
    transitions,
    highlight,
    locked = false,
    loading = false,
    readingDrive = false,
  }: PrinterCommandsPanelProps = $props();

  // How long the mark stays lit.
  const MARK_MS = 4000;

  let panel = $state<HTMLElement | null>(null);
  let marked = $state<string | undefined>(undefined);

  onMount(() => {
    void commands.loadPresets();
  });

  // Not `state`: that name shadows the `$state` rune for svelte2tsx.
  const printerState = $derived(
    device === undefined ? undefined : textAt(device, STATE_PATH),
  );

  const rows = $derived(
    verbsOf(device?.capabilities ?? null)
      .filter((entry) => isOfferedHere(entry.verb))
      .map((entry) => ({
        entry,
        projection: projectCommand(entry.verb, {
          capabilities: device?.capabilities ?? null,
          // The device-state vocabulary when the page fetched one.
          transitions: transitions ?? LIFECYCLE_TRANSITIONS,
          state: printerState,
          locked,
          permitted: device?.verbs ?? null,
        }) satisfies CommandProjection,
      })),
  );

  // The verb to mark, once there is a row carrying it.
  const wanted = $derived(
    rows.some((listing) => listing.entry.verb === highlight)
      ? highlight
      : undefined,
  );

  const refusal = $derived(
    commands.error === undefined ? undefined : describeFault(commands.error),
  );

  const send = (verb: string, args?: Readonly<Record<string, string>>) => {
    void commands.send(verb, args);
  };

  // The mark is state rather than a `$derived` of the prop because it.
  $effect(() => {
    const verb = wanted;
    if (verb === undefined) return;

    marked = verb;

    const row = panel?.querySelector(`[id="command-${verb}"]`);
    if (row instanceof HTMLElement) {
      // The stylesheet's reduced-motion block governs CSS animation.
      const reduced = window.matchMedia('(prefers-reduced-motion: reduce)');
      row.scrollIntoView({
        block: 'center',
        behavior: reduced.matches ? 'auto' : 'smooth',
      });
      // Focus is what says it to a screen reader.
      row.focus({ preventScroll: true });
    }

    const timer = setTimeout(() => {
      marked = undefined;
    }, MARK_MS);
    return () => {
      clearTimeout(timer);
    };
  });
</script>

<div
  bind:this={panel}
  class="@container/commands flex flex-col gap-6"
  aria-busy={commands.sending}
>
  <FaultAlert fault={refusal} />

  {#if loading}
    <!-- The panel's own shape (ADR-20260917-a7d rule 7). -->
    <div
      class="rounded-card flex flex-col gap-3 border border-border p-6"
      aria-label={t('printerDetailCommands.loading.label')}
      role="status"
    >
      <Skeleton class="h-5 w-32" />
      {#each [0, 1, 2, 3, 4] as row (row)}
        <Skeleton class="h-8 w-full" />
      {/each}
    </div>
  {:else}
    <CommandCatalogue
      {rows}
      {marked}
      sending={commands.sending}
      pending={commands.pending}
      {readingDrive}
      onSend={send}
    />
    <GcodeConsole {commands} />
  {/if}
</div>
