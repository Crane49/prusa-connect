<!--
  What the server could not read in full in a 3MF project. None of these
  stopped the read, so the rest of the page is still the project; each line
  says which part to trust less.

  It is one line that says how many things there are, and opens to list them.
  The list would push the project itself down the page, and most readers
  need only to know that something is there. A producer the server does not
  support leads, because it qualifies everything else the page shows.
-->
<script lang="ts">
  import TriangleAlertIcon from '@lucide/svelte/icons/triangle-alert';

  import { t } from '../i18n';
  import {
    Collapsible,
    CollapsibleContent,
    CollapsibleTrigger,
  } from '@prusa/connect-ui/primitives';
  import type { ProjectIssue } from './model';

  interface Props {
    issues: readonly ProjectIssue[];
    /** The list starts open, which a story uses to show it. */
    open?: boolean | undefined;
  }

  let { issues, open = false }: Props = $props();

  const ISSUE_KEY: Readonly<Record<ProjectIssue['code'], string>> = {
    unsupported_producer: 'projectView.issue.unsupported_producer',
    settings_unreadable: 'projectView.issue.settings_unreadable',
    outline_approx: 'projectView.issue.outline_approx',
    thumbnail_unreadable: 'projectView.issue.thumbnail_unreadable',
    unknown_reference: 'projectView.issue.unknown_reference',
    unsupported_extension: 'projectView.issue.unsupported_extension',
    gcode_only: 'projectView.issue.gcode_only',
  };

  const ORDER = Object.keys(ISSUE_KEY);

  // One line per kind and detail: a mesh too large to keep is reported once
  // per mesh, and a page needs to say it once.
  // A file of G-code alone is not a fault of the read, and the view says so
  // in a notice of its own.
  const lines = $derived(
    [
      ...new Map(
        issues
          .filter((issue) => issue.code !== 'gcode_only')
          .map((issue) => [`${issue.code}:${issue.detail ?? ''}`, issue]),
      ).values(),
    ]
      .sort((a, b) => ORDER.indexOf(a.code) - ORDER.indexOf(b.code))
      .map((issue) => ({
        key: `${issue.code}:${issue.detail ?? ''}`,
        text: t(ISSUE_KEY[issue.code], { detail: issue.detail ?? '' }),
      })),
  );
</script>

{#if lines.length > 0}
  <Collapsible
    {open}
    class="rounded-sm border border-warning bg-card text-sm"
    data-slot="project-issues"
  >
    <CollapsibleTrigger variant="inline" tone="warning">
      <span class="inline-flex min-w-0 items-center gap-2">
        <TriangleAlertIcon aria-hidden="true" class="size-4" />
        <span>{t('projectView.issues.summary', { count: lines.length })}</span>
      </span>
    </CollapsibleTrigger>
    <CollapsibleContent class="px-3 pbe-3">
      <ul
        class="grid list-disc grid-cols-1 content-start gap-1 ps-6 text-foreground"
      >
        {#each lines as line (line.key)}
          <li class="break-words">{line.text}</li>
        {/each}
      </ul>
    </CollapsibleContent>
  </Collapsible>
{/if}
