<!--
  What a person types into PrusaSlicer 2 for one printer: the address of the
  printer's PrusaLink routes on this server, and an API key. The key is an API
  credential of this server with the operate scope. It is shown once, when it
  is made, as the settings page shows a credential it mints. The host draws
  the card and its title around it.
-->
<script lang="ts">
  import KeyRoundIcon from '@lucide/svelte/icons/key-round';
  import {
    Alert,
    AlertDescription,
    Button,
    Collapsible,
    CollapsibleContent,
    CollapsibleTrigger,
    CopyField,
    FaultAlert,
  } from '@prusa/connect-ui/primitives';

  import {
    SaveToStorage,
    type SaveToStorageProps,
  } from '@prusa/connect-ui/shell';

  import type { FaultText } from '@prusa/connect-ui/i18n';

  import type { KeyState, SeenState, Translate } from './types';

  let {
    t,
    address,
    key,
    onCreate,
    keep,
    keepFault,
    seen = { kind: 'unknown' },
    keepReadOnly,
    keepElsewhere = false,
  }: {
    t: Translate;
    /** The address of the printer's PrusaLink routes, ending in a slash. */
    address: string;
    key: KeyState;
    /** Mints the API key. */
    onCreate: () => void;
    /**
     * Whether the printer's uploads are also saved on this server. Absent
     * until it is read, and when this server has no storage the person may
     * write to.
     */
    keep?: SaveToStorageProps | undefined;
    /** Why reading or changing it failed. */
    keepFault?: FaultText | undefined;
    /** Whether PrusaSlicer has reached this printer here, and when. */
    seen?: SeenState;
    /** The setting for a person who may not change it. */
    keepReadOnly?: { on: boolean; nobody: boolean } | undefined;
    /** The uploads go to a storage this person may not write to. */
    keepElsewhere?: boolean;
  } = $props();

  // PrusaSlicer sends the key in a header on every request, so over plain
  // HTTP anyone on the network can read it.
  const plain = $derived(address.startsWith('http:'));

  // The steps are open while PrusaSlicer has not reached the printer, and
  // folded once it has. It is decided once, so a person who opens or closes
  // them keeps what they chose.
  let stepsOpen = $state(false);
  let stepsDecided = false;
  $effect(() => {
    if (stepsDecided || seen.kind === 'unknown') return;
    stepsDecided = true;
    stepsOpen = seen.kind === 'never';
  });
</script>

<!-- Two columns on a wide card: reaching the printer from PrusaSlicer, and
     where its uploads go on this server. A narrow card stacks them. -->
<div class="@container" data-part="prusalink">
  <div class="grid gap-4 @3xl:grid-cols-[minmax(0,1fr)_18rem] @3xl:gap-x-6">
    <div class="grid min-w-0 content-start gap-4">
      {#if seen.kind !== 'unknown'}
        <!-- PrusaSlicer holds no connection open, so this is the last time it
             asked, not whether it is connected now. -->
        <p class="flex items-start gap-2 text-sm" data-part="seen">
          <span
            aria-hidden="true"
            class={[
              'mt-1.5 size-2 shrink-0 rounded-full',
              seen.kind === 'at' ? 'bg-success' : 'bg-muted-foreground',
            ]}
          ></span>
          {#if seen.kind === 'at'}
            <!-- Two lines on a narrow card, one on a wide one. -->
            <span>
              {t('prusalink.seen.connected')}<span class="hidden @md:inline"
                >,</span
              >
              <span class="block @md:inline">
                {t('prusalink.seen.lastSeen', { ago: seen.ago })}
              </span>
            </span>
          {:else}
            {t('prusalink.seen.never')}
          {/if}
        </p>
      {/if}
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

      <Collapsible bind:open={stepsOpen} data-part="steps">
        <CollapsibleTrigger variant="inline">
          {t('prusalink.steps.title')}
        </CollapsibleTrigger>
        <CollapsibleContent>
          <ol
            class="grid list-decimal gap-1 ps-5 pt-2 text-sm text-muted-foreground"
          >
            <li>{t('prusalink.steps.add')}</li>
            <li>{t('prusalink.steps.host')}</li>
            <li>{t('prusalink.steps.key')}</li>
            <li>{t('prusalink.steps.test')}</li>
          </ol>
        </CollapsibleContent>
      </Collapsible>
    </div>

    {#if keep !== undefined || keepFault !== undefined || keepReadOnly !== undefined}
      <!-- A setting of this server, not a step in PrusaSlicer, so a line
           parts it from the column beside it, or from the steps above. -->
      <section
        class="grid content-start gap-3 border-t border-border pt-4 @3xl:border-s @3xl:border-t-0 @3xl:ps-6 @3xl:pt-0"
        data-part="keep"
      >
        <div class="grid gap-1">
          <h4 class="text-sm font-medium">{t('prusalink.keep.title')}</h4>
          <!-- One setting for the printer, so a person knows it is not theirs
               alone. -->
          <p class="text-xs text-muted-foreground" data-part="keep-scope">
            {t('prusalink.keep.scope')}
          </p>
        </div>
        <FaultAlert fault={keepFault} />
        {#if keep !== undefined}
          <SaveToStorage {...keep} />
          {#if keepElsewhere}
            <p class="text-xs text-muted-foreground" data-part="keep-elsewhere">
              {t('prusalink.keep.elsewhere')}
            </p>
          {/if}
        {:else if keepReadOnly !== undefined}
          <p class="text-sm" data-part="keep-read-only">
            {keepReadOnly.on
              ? t('prusalink.keep.readOnlyOn')
              : t('prusalink.keep.readOnlyOff')}
          </p>
          <p class="text-xs text-muted-foreground">
            {keepReadOnly.nobody
              ? t('prusalink.keep.readOnlyNobody')
              : t('prusalink.keep.readOnlyNote')}
          </p>
        {/if}
      </section>
    {/if}
  </div>
</div>
