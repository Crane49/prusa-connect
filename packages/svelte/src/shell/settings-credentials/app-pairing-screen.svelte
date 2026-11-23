<!--
  The page an app sends a person to. It reads the pairing the app's code names
  and lets the signed-in person approve what the app asked for, or deny it
  (ADR-20261025-tpp). A code the link carried is never shown as a field, so
  the person is not led to copy it anywhere.
-->
<script lang="ts">
  import CircleCheckIcon from '@lucide/svelte/icons/circle-check';
  import CircleXIcon from '@lucide/svelte/icons/circle-x';
  import LinkIcon from '@lucide/svelte/icons/link';
  import TriangleAlertIcon from '@lucide/svelte/icons/triangle-alert';

  import { CREDENTIAL_SCOPES, type CredentialScope } from '../../data';
  import { describeFault, formatDateTime, t } from '../../i18n';
  import {
    Alert,
    AlertDescription,
    Button,
    Card,
    CardContent,
    CardFooter,
    CardHeader,
    CardTitle,
    EmptyState,
    FaultAlert,
    Field,
    Input,
    ListingSkeleton,
  } from '../../primitives';
  import { type AppPairingScreenProps, widestScope } from './types';

  let {
    review,
    linked,
    onLookup,
    onApprove,
    onDeny,
    credentialsHref,
  }: AppPairingScreenProps = $props();

  const headingId = $props.id();

  /** What each scope lets the app do, said about the app. */
  const CAN: Record<CredentialScope, string> = {
    read: 'appPairing.can.read',
    operate: 'appPairing.can.operate',
    administer: 'appPairing.can.administer',
  };

  // What the person types before a pairing is found. A code the link carried
  // is already looked up, so the field starts with it.
  let typed = $derived(review.code);

  const name = $derived(review.pairing?.app_name ?? '');

  /** The scopes the app asked for, which an approval sends unchanged. */
  const asked = $derived.by((): CredentialScope[] => {
    const named = review.pairing?.scopes ?? [];
    const known = CREDENTIAL_SCOPES.filter((scope) => named.includes(scope));

    return known.length > 0 ? known : [widestScope(named)];
  });

  // A link that named a pairing needs no field. One that named nothing, or a
  // code nothing answers, leaves the field so the person can type it.
  const askForCode = $derived(
    !linked || (review.pairing === undefined && !review.loading),
  );

  const heading = $derived.by((): string => {
    if (review.outcome === 'approved') {
      return t('appPairing.approved.title', { name });
    }
    if (review.outcome === 'denied') {
      return t('appPairing.denied.title', { name });
    }
    if (review.pairing !== undefined && !review.loading) {
      return t('appPairing.asks', { name });
    }

    return t('appPairing.title');
  });

  const fault = $derived(
    review.error === undefined ? undefined : describeFault(review.error),
  );

  const lookup = (event: SubmitEvent): void => {
    event.preventDefault();
    const code = typed.trim();
    if (code === '') return;
    onLookup(code);
  };
</script>

<Card role="region" aria-labelledby={headingId} class="mx-auto w-full max-w-xl">
  <CardHeader>
    <CardTitle>
      <h1 id={headingId} class="contents">{heading}</h1>
    </CardTitle>
  </CardHeader>

  <CardContent class="flex flex-col gap-4">
    {#if review.outcome === 'approved'}
      <Alert variant="success">
        <CircleCheckIcon aria-hidden="true" />
        <AlertDescription class="flex flex-col items-start gap-3">
          {t('appPairing.approved.note', { name })}
          <!-- A link drawn as the button, because the list is somewhere else. -->
          <Button variant="secondary">
            {#snippet child({ props }: { props: Record<string, unknown> })}
              <a {...props} href={credentialsHref}>
                {t('appPairing.approved.list')}
              </a>
            {/snippet}
          </Button>
        </AlertDescription>
      </Alert>
    {:else if review.outcome === 'denied'}
      <Alert>
        <CircleXIcon aria-hidden="true" />
        <AlertDescription>
          {t('appPairing.denied.note', { name })}
        </AlertDescription>
      </Alert>
    {:else}
      {#if askForCode}
        <form class="flex items-end gap-2" onsubmit={lookup}>
          <Field
            class="grow"
            label={t('appPairing.code')}
            description={t('appPairing.code.note')}
            control={Input}
            controlProps={{
              value: typed,
              autocomplete: 'off',
              autocapitalize: 'characters',
              spellcheck: false,
              placeholder: t('appPairing.code.placeholder'),
              oninput: (event: Event & { currentTarget: HTMLInputElement }) => {
                typed = event.currentTarget.value;
              },
            }}
          />
          <Button
            type="submit"
            variant="secondary"
            disabled={typed.trim() === ''}
          >
            {t('appPairing.code.find')}
          </Button>
        </form>
      {/if}

      {#if fault !== undefined}
        <FaultAlert {fault} />
      {/if}

      {#if review.loading}
        <ListingSkeleton label={t('appPairing.loading')} rows={2} />
      {:else if review.pairing !== undefined}
        <div class="flex flex-col gap-3">
          <p class="text-body">{t(CAN[widestScope(asked)])}</p>
          <p class="flex items-start gap-2 text-sm text-warning-text">
            <TriangleAlertIcon
              aria-hidden="true"
              class="mt-0.5 size-4 shrink-0"
            />
            {t('appPairing.trust', { name })}
          </p>
          <p class="text-xs text-muted-foreground">
            {t('appPairing.from', {
              address: review.pairing.client_address,
              time: formatDateTime(review.pairing.expires_at),
            })}
          </p>
        </div>
      {:else if !review.loaded}
        <EmptyState
          icon={LinkIcon}
          heading={t('appPairing.empty.heading')}
          headingLevel="h2"
          description={t('appPairing.empty.note')}
        />
      {/if}
    {/if}
  </CardContent>

  {#if review.pairing !== undefined && review.outcome === undefined && !review.loading}
    <CardFooter class="flex justify-end gap-2">
      <Button variant="secondary" disabled={review.deciding} onclick={onDeny}>
        {t('appPairing.deny')}
      </Button>
      <Button
        disabled={review.deciding}
        onclick={() => {
          onApprove(asked);
        }}
      >
        {t('appPairing.approve')}
      </Button>
    </CardFooter>
  {/if}
</Card>
