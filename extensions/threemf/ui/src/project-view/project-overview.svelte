<!--
  A 3MF project at a glance: the picture the slicer saved, who saved it and
  for which printer, the filaments it prints with, and how much it holds.

  Only the facts a person checks before printing stand here. The settings,
  the parts and what could not be read are one press away, below or behind
  the issue line.
-->
<script lang="ts">
  import BoxIcon from '@lucide/svelte/icons/box';

  import { formatUnit, t } from '../i18n';
  import {
    Badge,
    type BadgeVariant,
    MaterialSwatch,
    PrinterIcon,
  } from '@prusa/connect-ui/primitives';
  import { printerModelOf } from '../printers';
  import {
    headlineSettings,
    printersOf,
    type Project,
    type ProducerKind,
    producerKind,
    producerText,
    projectPicture,
  } from './model';
  import ProjectIssues from './project-issues.svelte';

  interface Props {
    project: Project;
    /** Where the picture at an index of the project's thumbnails is served. */
    thumbnailUrl?: ((index: number) => string) | undefined;
    /**
     * Whether the overview draws the project's picture. A file's page shows
     * the picture in its header, so the file view leaves it out here
     * (ADR-20261027-4tq rule 19).
     */
    picture?: boolean | undefined;
  }

  let { project, thumbnailUrl, picture: drawPicture = true }: Props = $props();
  const uid = $props.id();

  const BADGE: Readonly<Record<ProducerKind, BadgeVariant>> = {
    prusaslicer: 'secondary',
    prusaslicerPreview: 'secondary',
    orcaslicer: 'secondary',
    unsupported: 'warning',
    generic: 'outline',
  };

  const kind = $derived(producerKind(project.producer));
  const named = $derived(producerText(project.producer));

  const producer = $derived.by(() => {
    if (kind !== 'generic') {
      return named === '' ? t('projectView.producer.unknown') : named;
    }

    return named === ''
      ? t('projectView.producer.generic')
      : t('projectView.producer.genericNamed', { name: named });
  });

  const printers = $derived(printersOf(project));
  const picture = $derived(projectPicture(project));
  const title = $derived(
    (project.title ?? '') === ''
      ? t('projectView.overview.heading')
      : (project.title ?? ''),
  );

  // The filaments the project prints with: of the first setup, only the
  // extruders some object uses, so a five-tool printer profile does not list
  // four spools a one-colour print never touches.
  const filaments = $derived.by(() => {
    const all = project.setups[0]?.settings.filaments ?? [];
    const used = new Set(project.extruders ?? []);

    return all.flatMap((filament, at) =>
      used.size > 0 && !used.has(at + 1) ? [] : [{ filament, at }],
    );
  });

  const settings = $derived(headlineSettings(project));

  const facts = $derived(
    [
      {
        id: 'beds',
        text: t('projectView.overview.beds', { count: project.counts.beds }),
        warn: false,
      },
      {
        id: 'objects',
        // A file of G-code alone has none, and saying so says nothing.
        text:
          project.counts.objects === 0
            ? ''
            : t('projectView.overview.objects', {
                count: project.counts.objects,
              }),
        warn: false,
      },
      {
        id: 'copies',
        text:
          project.counts.instances === 0
            ? ''
            : t('projectView.overview.copies', {
                count: project.counts.instances,
              }),
        warn: false,
      },
      {
        id: 'layer',
        text:
          settings.layer === undefined || settings.layer === 0
            ? ''
            : t('projectView.overview.layer', {
                height: formatUnit(settings.layer, 'mm', {
                  maximumFractionDigits: 2,
                }),
              }),
        warn: false,
      },
      {
        id: 'nozzle',
        text:
          settings.nozzles.length === 0
            ? ''
            : t('projectView.overview.nozzle', {
                diameter: settings.nozzles
                  .map((diameter) =>
                    formatUnit(diameter, 'mm', { maximumFractionDigits: 2 }),
                  )
                  .join(', '),
              }),
        warn: false,
      },
      {
        id: 'parked',
        text:
          project.counts.non_printable === 0
            ? ''
            : t('projectView.overview.parked', {
                count: project.counts.non_printable,
              }),
        warn: true,
      },
      {
        id: 'outside',
        text:
          project.counts.outside + project.counts.partly_outside === 0
            ? ''
            : t('projectView.overview.outside', {
                count: project.counts.outside + project.counts.partly_outside,
              }),
        warn: true,
      },
    ].filter((fact) => fact.text !== ''),
  );
</script>

<section
  class="@container/overview"
  data-slot="project-overview"
  aria-labelledby="{uid}-title"
>
  <div
    class="grid grid-cols-1 content-start gap-5 @2xl/overview:flex @2xl/overview:items-start"
  >
    {#if drawPicture}
      <div
        class="flex aspect-square w-full max-w-72 shrink-0 items-center justify-center self-center overflow-hidden rounded-md border border-border bg-muted @2xl/overview:w-56 @2xl/overview:self-start"
      >
        {#if picture !== undefined && thumbnailUrl !== undefined}
          <img
            src={thumbnailUrl(picture)}
            alt={t('projectView.overview.picture')}
            class="size-full object-contain"
          />
        {:else}
          <BoxIcon aria-hidden="true" class="size-16 text-muted-foreground" />
        {/if}
      </div>
    {/if}

    <div class="grid min-w-0 flex-1 grid-cols-1 content-start gap-4">
      <div class="grid min-w-0 grid-cols-1 content-start gap-2">
        <h2 id="{uid}-title" class="text-lg font-semibold break-words">
          {title}
        </h2>
        <div class="flex flex-wrap items-center gap-x-4 gap-y-2 text-sm">
          {#each printers as printer (printer)}
            <span class="inline-flex min-w-0 items-center gap-1.5 font-medium">
              <PrinterIcon
                model={printerModelOf(printer)}
                reported={printer}
                class="size-5 shrink-0"
              />
              <span class="truncate">{printer}</span>
            </span>
          {:else}
            <span class="text-muted-foreground">
              {t('projectView.summary.noPrinter')}
            </span>
          {/each}
          <span class="inline-flex min-w-0 flex-wrap items-center gap-1.5">
            <span class="text-muted-foreground">
              {t('projectView.summary.savedBy')}
            </span>
            <Badge variant={BADGE[kind]} class="max-w-full">
              <span class="truncate">{producer}</span>
            </Badge>
            {#if kind === 'prusaslicerPreview'}
              <Badge variant="info">{t('projectView.producer.preview')}</Badge>
            {:else if kind === 'unsupported'}
              <Badge variant="warning">
                {t('projectView.producer.unsupported')}
              </Badge>
            {/if}
          </span>
        </div>
      </div>

      {#if filaments.length > 0}
        <ul
          class="flex flex-wrap gap-2"
          aria-label={t('projectView.settings.filaments')}
        >
          {#each filaments as row (row.at)}
            <li
              class="inline-flex items-center gap-2 rounded-sm border border-border py-1 ps-1.5 pe-2.5 text-sm"
            >
              <MaterialSwatch
                class="size-5"
                material={{
                  name: row.filament.type ?? '',
                  colorRgba: row.filament.colour,
                  colorName: row.filament.colour,
                }}
              />
              <span
                class="font-mono text-xs text-muted-foreground tabular-nums"
              >
                {t('projectView.toolNumber', { n: row.at })}
              </span>
              <span class="font-medium">
                {row.filament.type ?? t('projectView.overview.noMaterial')}
              </span>
            </li>
          {/each}
        </ul>
      {/if}

      <ul
        class="flex flex-wrap gap-x-5 gap-y-1 text-sm"
        aria-label={t('projectView.overview.facts')}
      >
        {#each facts as fact (fact.id)}
          <li
            class={[
              'tabular-nums',
              fact.warn ? 'text-warning-text' : 'text-muted-foreground',
            ]}
          >
            {fact.text}
          </li>
        {/each}
      </ul>

      <ProjectIssues issues={project.issues} />
    </div>
  </div>
</section>
