<!--
  The parts being made from dropped files, one row each.

  A row says where its part is: waiting for its turn, uploading, added, or
  refused. A part that was added links to itself, and a refusal is drawn by
  its code. A failed part that could not be taken back out says what is left:
  the part, which links to its page so it can be deleted there, or files in
  the storage. The browser reports no progress while a request body goes up, so
  a row spins rather than drawing a bar that would sit at zero.
-->
<script lang="ts">
  import CheckIcon from '@lucide/svelte/icons/check';
  import CircleAlertIcon from '@lucide/svelte/icons/circle-alert';
  import LoaderCircleIcon from '@lucide/svelte/icons/loader-circle';
  import XIcon from '@lucide/svelte/icons/x';
  import { formatList } from '@prusa/connect-ui/i18n';
  import { Button, ClippedName, Spinner } from '@prusa/connect-ui/primitives';

  import type { DropJob } from '../folder-drop.svelte';
  import { faultText } from '../faults';
  import { TOP } from '../navigation.svelte';
  import { heldSession } from '../session';
  import FolderLink from './folder-link.svelte';

  const { catalogue, drop, host } = heldSession();
  const t = host.t;

  const STATE: Record<DropJob['state'], string> = {
    waiting: 'parts.drop.state.waiting',
    sending: 'parts.drop.state.sending',
    done: 'parts.drop.state.done',
    failed: 'parts.drop.state.failed',
    refused: 'parts.drop.state.refused',
  };

  const accepted = $derived(
    formatList(catalogue.storage?.allowed_extensions ?? []),
  );
</script>

{#if drop.jobs.length > 0}
  <section
    class="grid gap-y-2 rounded-sm border border-border bg-surface p-3"
    aria-labelledby="parts-drop"
    data-part="drop-tray"
  >
    <div class="flex items-center justify-between gap-2">
      <h2 id="parts-drop" class="text-sm font-medium">
        {t('parts.drop.heading')}
      </h2>
      {#if !drop.sending}
        <Button
          variant="ghost"
          size="icon-sm"
          label={t('parts.drop.clear')}
          onclick={() => {
            drop.clear();
          }}
        >
          <XIcon aria-hidden="true" />
        </Button>
      {/if}
    </div>
    <ul class="grid gap-y-1" aria-live="polite">
      {#each drop.jobs as job (job.key)}
        {@const refusal =
          job.fault === undefined ? undefined : faultText(job.fault, t)}
        {@const leftover =
          job.leftFault === undefined ? undefined : faultText(job.leftFault, t)}
        <li
          class="grid min-w-0 gap-y-0.5 text-sm"
          data-part="drop-job"
          data-state={job.state}
        >
          <span class="flex min-w-0 items-center gap-2">
            {#if job.state === 'done'}
              <CheckIcon
                aria-hidden="true"
                class="size-4 shrink-0 text-success-text"
              />
            {:else if job.state === 'failed' || job.state === 'refused'}
              <CircleAlertIcon
                aria-hidden="true"
                class="size-4 shrink-0 text-destructive-text"
              />
            {:else if job.state === 'sending'}
              <Spinner size="sm" tone="muted" class="shrink-0" />
            {:else}
              <LoaderCircleIcon
                aria-hidden="true"
                class="size-4 shrink-0 text-muted-foreground"
              />
            {/if}
            {#if (job.state === 'done' || job.left === 'part') && job.part !== undefined}
              <FolderLink
                spot={{ ...TOP, folder: job.folder, part: job.part }}
                class="min-w-0 flex-1 rounded-xs underline-offset-2 outline-none hover:underline focus-visible:ring-2 focus-visible:ring-ring"
              >
                <ClippedName name={job.name} />
              </FolderLink>
            {:else}
              <ClippedName name={job.name} class="min-w-0 flex-1" />
            {/if}
            <span class="text-xs whitespace-nowrap text-muted-foreground">
              {t('parts.drop.files', { count: job.files.length })} · {t(
                STATE[job.state],
              )}
            </span>
          </span>
          {#if job.state === 'refused'}
            <span class="ps-6 text-xs text-destructive-text">
              {t('parts.drop.refused', { extensions: accepted })}
            </span>
          {:else if refusal}
            <span class="ps-6 text-xs text-destructive-text">
              {refusal.title}{#if refusal.code}<code class="ms-1"
                  >{refusal.code}</code
                >{/if}
            </span>
          {/if}
          {#if job.left !== undefined}
            <span
              class="ps-6 text-xs text-destructive-text"
              data-part="drop-left"
            >
              {job.left === 'part'
                ? t('parts.drop.leftPart')
                : t('parts.drop.leftFiles', { dir: job.leftIn ?? '' })}
              {#if leftover?.code}<code class="ms-1">{leftover.code}</code>{/if}
            </span>
          {/if}
        </li>
      {/each}
    </ul>
  </section>
{/if}
