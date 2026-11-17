<!-- What one job's row can do: open its detail, open its G-code, cancel it. -->
<script lang="ts">
  import { DropdownMenuItem, RowActions } from '@prusa/connect-ui/primitives';
  import CircleXIcon from '@lucide/svelte/icons/circle-x';
  import FileCodeIcon from '@lucide/svelte/icons/file-code';
  import InfoIcon from '@lucide/svelte/icons/info';

  import type { Job, Translate } from '../../api';
  import { fileHref, isActive, projectOf } from '../../jobs';

  let {
    job,
    detailHref,
    onCancel,
    t,
  }: {
    job: Job;
    detailHref: string;
    onCancel: (id: string) => void;
    t: Translate;
  } = $props();
</script>

<RowActions name={projectOf(job)}>
  <DropdownMenuItem>
    {#snippet child({ props })}
      <a {...props} href={detailHref}>
        <InfoIcon aria-hidden="true" />
        {t('slicer.action.details')}
      </a>
    {/snippet}
  </DropdownMenuItem>
  {#if job.result}
    {@const result = job.result}
    <DropdownMenuItem>
      {#snippet child({ props })}
        <a {...props} href={fileHref(result.storage, result.path)}>
          <FileCodeIcon aria-hidden="true" />
          {t('slicer.action.openOutput')}
        </a>
      {/snippet}
    </DropdownMenuItem>
  {/if}
  {#if isActive(job)}
    <DropdownMenuItem variant="destructive" onSelect={() => onCancel(job.id)}>
      <CircleXIcon aria-hidden="true" />
      {t('slicer.action.cancel')}
    </DropdownMenuItem>
  {/if}
</RowActions>
