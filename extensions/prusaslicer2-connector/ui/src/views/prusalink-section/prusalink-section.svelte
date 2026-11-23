<!--
  What a person types into PrusaSlicer 2 for one printer: the address of the
  printer's PrusaLink routes on this server, and an API key. The key is an API
  credential of this server with the operate scope. It is shown once, when it
  is made, as the settings page shows a credential it mints.
-->
<script lang="ts">
  import KeyRoundIcon from '@lucide/svelte/icons/key-round';
  import {
    Alert,
    AlertDescription,
    Button,
    Card,
    CardContent,
    CardDescription,
    CardHeader,
    CardTitle,
    CopyField,
    FaultAlert,
  } from '@prusa/connect-ui/primitives';

  import type { KeyState, Translate } from './types';

  let {
    t,
    address,
    key,
    onCreate,
  }: {
    t: Translate;
    /** The address of the printer's PrusaLink routes, ending in a slash. */
    address: string;
    key: KeyState;
    /** Mints the API key. */
    onCreate: () => void;
  } = $props();

  // PrusaSlicer sends the key in a header on every request, so over plain
  // HTTP anyone on the network can read it.
  const plain = $derived(address.startsWith('http:'));
</script>

<Card data-part="prusalink">
  <CardHeader>
    <CardTitle>{t('prusalink.title')}</CardTitle>
    <CardDescription>{t('prusalink.intro')}</CardDescription>
  </CardHeader>
  <CardContent class="grid gap-4">
    <CopyField label={t('prusalink.address')} value={address} />

    {#if key.kind === 'created'}
      <Alert variant="warning">
        <KeyRoundIcon aria-hidden="true" />
        <AlertDescription class="grid gap-3">
          {t('prusalink.created')}
          {#if plain}
            <span data-part="plain-http">{t('prusalink.plainHttp')}</span>
          {/if}
          <CopyField
            label={t('prusalink.key')}
            value={key.token}
            class="w-full"
          />
        </AlertDescription>
      </Alert>
    {:else}
      <FaultAlert fault={key.kind === 'failed' ? key.fault : undefined} />
      <div>
        <Button
          variant="secondary"
          disabled={key.kind === 'creating'}
          onclick={onCreate}
        >
          <KeyRoundIcon aria-hidden="true" />
          {key.kind === 'creating'
            ? t('prusalink.creating')
            : t('prusalink.create')}
        </Button>
      </div>
    {/if}

    <section class="grid gap-2">
      <h4 class="text-sm font-medium">{t('prusalink.steps.title')}</h4>
      <ol class="grid list-decimal gap-1 ps-5 text-sm text-muted-foreground">
        <li>{t('prusalink.steps.add')}</li>
        <li>{t('prusalink.steps.host')}</li>
        <li>{t('prusalink.steps.key')}</li>
        <li>{t('prusalink.steps.test')}</li>
      </ol>
    </section>
  </CardContent>
</Card>
