<!--
  The page an app sends a person to. It reads the pairing the app's code names
  and lets the signed-in person approve it, with the scope the app asked for or
  a narrower one, or deny it (ADR-20261025-tpp).
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
    AlertTitle,
    Button,
    Card,
    CardContent,
    CardDescription,
    CardFooter,
    CardHeader,
    CardTitle,
    EmptyState,
    FaultAlert,
    Field,
    Input,
    KeyValueList,
    ListingSkeleton,
  } from '../../primitives';
  import ScopeChoice from './scope-choice.svelte';
  import { type AppPairingScreenProps, widestScope } from './types';

  let {
    review,
    onLookup,
    onApprove,
    onDeny,
    credentialsHref,
  }: AppPairingScreenProps = $props();

  const headingId = $props.id();

  // What the person types before a pairing is found. A code the link carried
  // is already looked up, so the field starts with it.
  let typed = $derived(review.code);

  /** The widest scope the app asked for, and every narrower one beside it. */
  const asked = $derived(widestScope(review.pairing?.scopes));
  const offered = $derived(
    CREDENTIAL_SCOPES.slice(0, CREDENTIAL_SCOPES.indexOf(asked) + 1),
  );

  // The person's choice, the app's own wish until they change it.
  let chosen = $state<CredentialScope | undefined>(undefined);
  const scope = $derived(chosen ?? asked);

  const fault = $derived(
    review.error === undefined ? undefined : describeFault(review.error),
  );

  const lookup = (event: SubmitEvent): void => {
    event.preventDefault();
    const code = typed.trim();
    if (code === '') return;
    chosen = undefined;
    onLookup(code);
  };
</script>

<Card role="region" aria-labelledby={headingId} class="mx-auto w-full max-w-xl">
  <CardHeader>
    <CardTitle>
      <h1 id={headingId} class="contents">{t('appPairing.title')}</h1>
    </CardTitle>
    <CardDescription>{t('appPairing.note')}</CardDescription>
  </CardHeader>

  <CardContent class="flex flex-col gap-4">
    {#if review.outcome === 'approved'}
      <Alert variant="success">
        <CircleCheckIcon aria-hidden="true" />
        <AlertTitle>{t('appPairing.approved.title')}</AlertTitle>
        <AlertDescription class="flex flex-col items-start gap-3">
          {t('appPairing.approved.note', {
            name: review.pairing?.app_name ?? '',
          })}
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
        <AlertTitle>{t('appPairing.denied.title')}</AlertTitle>
        <AlertDescription>
          {t('appPairing.denied.note', {
            name: review.pairing?.app_name ?? '',
          })}
        </AlertDescription>
      </Alert>
    {:else}
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

      {#if fault !== undefined}
        <FaultAlert {fault} />
      {/if}

      {#if review.loading}
        <ListingSkeleton label={t('appPairing.loading')} rows={2} />
      {:else if review.pairing !== undefined}
        <section
          class="flex flex-col gap-4"
          aria-labelledby={`${headingId}-app`}
        >
          <h2 id={`${headingId}-app`} class="text-lg font-medium">
            {t('appPairing.asks', { name: review.pairing.app_name })}
          </h2>
          <KeyValueList
            rows={[
              {
                id: 'address',
                label: t('appPairing.address'),
                value: review.pairing.client_address,
                mono: true,
              },
              {
                id: 'expires',
                label: t('appPairing.expires'),
                value: formatDateTime(review.pairing.expires_at),
              },
            ]}
          />
          <Alert variant="warning">
            <TriangleAlertIcon aria-hidden="true" />
            <AlertTitle>{t('appPairing.trust.title')}</AlertTitle>
            <AlertDescription>{t('appPairing.trust.note')}</AlertDescription>
          </Alert>
          <fieldset class="flex flex-col gap-2">
            <legend class="text-body font-medium">
              {t('appPairing.scope')}
            </legend>
            <ScopeChoice
              {offered}
              value={scope}
              label={t('appPairing.scope')}
              onChange={(next: CredentialScope) => {
                chosen = next;
              }}
            />
          </fieldset>
        </section>
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
          onApprove(scope);
        }}
      >
        {t('appPairing.approve')}
      </Button>
    </CardFooter>
  {/if}
</Card>
