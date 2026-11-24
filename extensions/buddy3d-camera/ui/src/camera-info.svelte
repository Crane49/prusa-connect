<!--
  The Information tab of a Buddy3D camera: what it last reported about
  itself, its network and its SD card. A person who may operate a connected
  camera renames it here.
-->
<script lang="ts">
  import { Button, CopyButton, Input } from '@prusa/connect-ui/primitives';

  import type { PairedCamera } from './api';
  import { held } from './held';

  let { paired, controls }: { paired: PairedCamera; controls: boolean } = $props();

  const { store, t } = held();

  const status = $derived(paired.status);
  const card = $derived(status?.sd_card);
  const renames = $derived(controls && paired.features?.includes('CameraName') === true);

  // What is typed, until the camera reports another name.
  let name = $derived(status?.name ?? '');

  const size = (mb: number | undefined): string | undefined =>
    mb === undefined ? undefined : t('buddy3d.gigabytes', { size: (mb / 1024).toFixed(1) });

  const system = $derived([
    ...(renames ? [] : [{ id: 'name', label: t('buddy3d.name'), value: status?.name }]),
    { id: 'model', label: t('buddy3d.model'), value: paired.model },
    { id: 'firmware', label: t('buddy3d.firmware'), value: status?.firmware },
    { id: 'hardware', label: t('buddy3d.hardware'), value: status?.hardware },
    { id: 'uptime', label: t('buddy3d.uptime'), value: status?.uptime },
    {
      id: 'temperature',
      label: t('buddy3d.temperature'),
      value: status?.temperature ? t('buddy3d.celsius', { degrees: Math.round(status.temperature) }) : undefined,
    },
  ]);
  const network = $derived([
    { id: 'ssid', label: t('buddy3d.wifi'), value: status?.ssid },
    {
      id: 'signal',
      label: t('buddy3d.signalQuality'),
      value: status?.signal_quality ? t('buddy3d.percent', { percent: status.signal_quality }) : undefined,
    },
    { id: 'ipv4', label: t('buddy3d.ipv4'), value: status?.address, copyable: true },
    { id: 'ipv6', label: t('buddy3d.ipv6'), value: status?.ipv6, copyable: true },
    { id: 'mac', label: t('buddy3d.mac'), value: status?.mac, copyable: true },
  ]);
  const sd = $derived([
    { id: 'status', label: t('buddy3d.sdStatus'), value: card?.detected ? card.status : t('buddy3d.sdMissing') },
    ...(card?.detected
      ? [
          { id: 'size', label: t('buddy3d.sdSize'), value: size(card.size) },
          { id: 'free', label: t('buddy3d.sdFree'), value: size(card.free) },
          { id: 'used', label: t('buddy3d.sdUsed'), value: size(card.used) },
        ]
      : []),
  ]);
</script>

<!-- Not KeyValueList: a module's own flex-col outranks its row layout. -->
{#snippet list(rows: readonly { id: string; label: string; value?: string | undefined; copyable?: boolean }[])}
  <dl class="grid max-w-md grid-cols-2 gap-x-4 gap-y-1">
    {#each rows as row (row.id)}
      <dt class="text-muted-foreground">{row.label}</dt>
      <dd class="flex min-w-0 items-center gap-1 break-all">
        {row.value ?? '—'}
        {#if row.copyable && row.value}
          <CopyButton value={row.value} label={t('buddy3d.copy', { name: row.label })} />
        {/if}
      </dd>
    {/each}
  </dl>
{/snippet}

<div class="flex flex-col gap-5" data-part="info">
  <section class="flex flex-col gap-2">
    <h4 class="font-medium">{t('buddy3d.system')}</h4>
    {#if renames}
      <form
        class="flex max-w-md items-center gap-2"
        onsubmit={(event) => {
          event.preventDefault();
          void store.control(paired.name, { name: name.trim() });
        }}
      >
        <Input
          aria-label={t('buddy3d.name')}
          placeholder={t('buddy3d.name')}
          maxlength={32}
          value={name}
          oninput={(event: Event & { currentTarget: HTMLInputElement }) => {
            name = event.currentTarget.value;
          }}
        />
        <Button
          type="submit"
          size="sm"
          variant="secondary"
          disabled={store.busy || name.trim() === '' || name.trim() === status?.name}
        >
          {t('buddy3d.rename')}
        </Button>
      </form>
    {/if}
    {@render list(system)}
  </section>

  <section class="flex flex-col gap-2">
    <h4 class="font-medium">{t('buddy3d.network')}</h4>
    {@render list(network)}
  </section>

  {#if card !== undefined}
    <section class="flex flex-col gap-2">
      <h4 class="font-medium">{t('buddy3d.sdCard')}</h4>
      {@render list(sd)}
    </section>
  {/if}
</div>
