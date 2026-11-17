<script module lang="ts">
  import AlertTriangleIcon from '@lucide/svelte/icons/alert-triangle';
  import CheckCircle2Icon from '@lucide/svelte/icons/check-circle-2';
  import InfoIcon from '@lucide/svelte/icons/info';
  import XCircleIcon from '@lucide/svelte/icons/x-circle';
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import AlertDescription from './alert-description.svelte';
  import AlertTitle from './alert-title.svelte';
  import Alert from './alert.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/Alert',
    component: Alert,
  });
</script>

{#snippet allVariants()}
  <div class="flex flex-col gap-3">
    <Alert variant="default">
      <InfoIcon />
      <AlertTitle>Firmware update available</AlertTitle>
      <AlertDescription>
        Version 5.4.1 is ready to install on this printer.
      </AlertDescription>
    </Alert>
    <Alert variant="destructive">
      <XCircleIcon />
      <AlertTitle>Nozzle clog detected</AlertTitle>
      <AlertDescription>
        The print has been paused. Clear the nozzle before resuming.
      </AlertDescription>
    </Alert>
    <Alert variant="success">
      <CheckCircle2Icon />
      <AlertTitle>Print complete</AlertTitle>
      <AlertDescription>benchy.3mf finished after 47 minutes.</AlertDescription>
    </Alert>
    <Alert variant="warning">
      <AlertTriangleIcon />
      <AlertTitle>Filament running low</AlertTitle>
      <AlertDescription>
        Less than 2 meters remain on the active spool.
      </AlertDescription>
    </Alert>
    <Alert variant="info">
      <InfoIcon />
      <AlertTitle>Camera stream idle</AlertTitle>
      <AlertDescription>No viewers connected in the last hour.</AlertDescription
      >
    </Alert>
  </div>
{/snippet}

<Story name="Default" asChild>
  <Alert>
    <InfoIcon />
    <AlertTitle>Firmware update available</AlertTitle>
    <AlertDescription>
      Version 5.4.1 is ready to install on this printer.
    </AlertDescription>
  </Alert>
</Story>

<Story name="All Variants" asChild children={allVariants} />

<Story name="Announces As An Alert" tags={['!dev', '!autodocs']} asChild>
  <Alert variant="destructive">
    <XCircleIcon />
    <AlertTitle>Nozzle clog detected</AlertTitle>
  </Alert>
</Story>

<Story
  name="Status Variants Clear AA Contrast"
  tags={['!dev', '!autodocs']}
  asChild
  children={allVariants}
/>
