<!--
  Settings → Extensions → Buddy3D cameras: where cameras send, and every
  paired camera with what it last reported.
-->
<script lang="ts">
  import { Alert, AlertDescription, Badge } from '@prusa/connect-ui/primitives';
  import { onDestroy } from 'svelte';

  import { held } from './held';

  const { store, t } = held();
  onDestroy(store.watch());

  const when = (value: string | undefined): string =>
    value === undefined ? '—' : new Date(value).toLocaleString();
</script>

<div class="flex flex-col gap-6">
  {#if store.status !== undefined}
    <section class="flex flex-col gap-1 text-sm" data-part="server">
      <p>
        {t('buddy3d.address', { address: `${store.status.address}:${String(store.status.port)}` })}
        {#if store.status.listening}
          <Badge variant="success">{t('buddy3d.listening')}</Badge>
        {:else}
          <Badge variant="destructive">{t('buddy3d.notListening')}</Badge>
        {/if}
      </p>
      {#if store.status.problem}
        <p class="text-destructive">{store.status.problem}</p>
      {/if}
    </section>
  {/if}

  {#if store.problem !== ''}
    <Alert variant="destructive"><AlertDescription>{store.problem}</AlertDescription></Alert>
  {/if}

  {#if store.loaded && store.cameras.length === 0}
    <p class="text-sm text-muted-foreground">{t('buddy3d.noneAnywhere')}</p>
  {/if}

  {#each store.cameras as camera (camera.name)}
    <section class="flex flex-col gap-1 rounded-md border p-3 text-sm" data-part="camera">
      <p class="flex items-center gap-2">
        <span class="font-medium">{camera.name}</span>
        {#if camera.connected}
          <Badge variant="success">{t('buddy3d.connected')}</Badge>
        {:else}
          <Badge variant="secondary">{t('buddy3d.offline')}</Badge>
        {/if}
      </p>
      <p class="text-muted-foreground">{t('buddy3d.printer', { printer: camera.printer })}</p>
      <p class="text-muted-foreground">{t('buddy3d.lastFrame', { when: when(camera.last_frame) })}</p>
      {#if camera.status}
        <p class="text-muted-foreground">
          {t('buddy3d.report', {
            firmware: camera.status.firmware ?? '—',
            interval: camera.status.snapshot_interval ?? 0,
            temperature: Math.round(camera.status.temperature ?? 0),
          })}
        </p>
      {/if}
      {#if camera.problem}
        <p class="text-destructive">{camera.problem}</p>
      {/if}
    </section>
  {/each}
</div>
