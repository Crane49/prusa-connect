<!-- Whether people sign in, and the first administrator when they do. -->
<script lang="ts">
  import CircleCheckIcon from '@lucide/svelte/icons/circle-check';
  import CircleMinusIcon from '@lucide/svelte/icons/circle-minus';
  import CircleQuestionMarkIcon from '@lucide/svelte/icons/circle-question-mark';
  import CircleXIcon from '@lucide/svelte/icons/circle-x';
  import KeyRoundIcon from '@lucide/svelte/icons/key-round';
  import LockIcon from '@lucide/svelte/icons/lock';
  import LockOpenIcon from '@lucide/svelte/icons/lock-open';
  import PlugZapIcon from '@lucide/svelte/icons/plug-zap';

  import type { SetupFlow } from '../../data';
  import { describeFault, t } from '../../i18n';
  import {
    Button,
    Card,
    CardContent,
    FaultAlert,
    Field,
    Input,
    Spinner,
  } from '../../primitives';
  import ChoiceCards from './choice-cards.svelte';
  import { identityErrors, oidcAnswers, oidcErrors } from './checks';
  import type { Oidc, SetupDraft, SetupMode } from './setup-draft.svelte';

  interface StepIdentityProps {
    draft: SetupDraft;
    showErrors: boolean;
    docsHref?: string | undefined;
    /** The address the issuer sends people back to when none is typed. */
    redirectFallback?: string | undefined;
    /** Tries the sign-in provider on the server. */
    flow?: SetupFlow | undefined;
  }

  let {
    draft,
    showErrors,
    docsHref,
    redirectFallback,
    flow,
  }: StepIdentityProps = $props();

  const tried = $derived(flow?.oidc);
  const tryFault = $derived(
    flow?.oidcError === undefined ? undefined : describeFault(flow.oidcError),
  );

  const tryProvider = (): void => {
    if (flow === undefined) return;
    const answers = oidcAnswers(draft, redirectFallback);
    void flow.checkOidc(draft.code.trim(), {
      issuer: answers['core.auth.issuer'] ?? '',
      client_id: answers['core.auth.client_id'] ?? '',
      redirect_uri: answers['core.auth.redirect_uri'] ?? '',
      ...(draft.oidcSecret === '' ? {} : { client_secret: draft.oidcSecret }),
    });
  };

  // Spelled out so every line the step can show is one the catalogue holds.
  const STEP_TEXT = $derived({
    discovery: {
      passed: t('setupWizard.oidc.step.discovery.passed'),
      failed: t('setupWizard.oidc.step.discovery.failed'),
      unknown: t('setupWizard.oidc.step.discovery.unknown'),
      skipped: t('setupWizard.oidc.step.discovery.skipped'),
    },
    client: {
      passed: t('setupWizard.oidc.step.client.passed'),
      failed: t('setupWizard.oidc.step.client.failed'),
      unknown: t('setupWizard.oidc.step.client.unknown'),
      skipped: t('setupWizard.oidc.step.client.skipped'),
    },
    redirect: {
      passed: t('setupWizard.oidc.step.redirect.passed'),
      failed: t('setupWizard.oidc.step.redirect.failed'),
      unknown: t('setupWizard.oidc.step.redirect.unknown'),
      skipped: t('setupWizard.oidc.step.redirect.skipped'),
    },
  });

  const STEP_ICON = {
    passed: CircleCheckIcon,
    failed: CircleXIcon,
    unknown: CircleQuestionMarkIcon,
    skipped: CircleMinusIcon,
  } as const;

  const STEP_TONE = {
    passed: 'text-success-text',
    failed: 'text-destructive',
    unknown: 'text-muted-foreground',
    skipped: 'text-muted-foreground',
  } as const;

  const triedSteps = $derived(
    tried === undefined
      ? []
      : [
          { id: 'discovery' as const, step: tried.discovery },
          { id: 'client' as const, step: tried.client },
          { id: 'redirect' as const, step: tried.redirect },
        ],
  );

  const options = $derived([
    {
      value: 'required' as const,
      title: t('setupWizard.identity.required.title'),
      note: t('setupWizard.identity.required.note'),
      icon: LockIcon,
      recommended: true,
    },
    {
      value: 'oidc' as const,
      title: t('setupWizard.identity.oidc.title'),
      note: t('setupWizard.identity.oidc.note'),
      icon: KeyRoundIcon,
    },
    {
      value: 'open' as const,
      title: t('setupWizard.identity.open.title'),
      note: t('setupWizard.identity.open.note'),
      icon: LockOpenIcon,
    },
  ]);

  const errors = $derived(identityErrors(draft));
  const ssoErrors = $derived(oidcErrors(draft));
  // The provider can be tried once the three things a sign-in starts from
  // read right; the administrator's subject plays no part in it.
  const triable = $derived(
    ssoErrors.issuer === '' &&
      ssoErrors.clientId === '' &&
      ssoErrors.redirectUri === '',
  );
  // The mismatch shows as soon as the second password is typed.
  const confirmError = $derived(
    showErrors || draft.confirmation !== '' ? errors.confirmation : '',
  );

  const ssoField = (
    name: keyof Oidc,
    placeholder?: string,
  ): Record<string, unknown> => ({
    value: draft.oidc[name],
    type: 'text',
    autocomplete: 'off',
    spellcheck: false,
    ...(placeholder === undefined ? {} : { placeholder }),
    oninput: (event: Event & { currentTarget: HTMLInputElement }) => {
      draft.sso({ [name]: event.currentTarget.value });
      // What was tried is no longer what is typed.
      if (name !== 'subject') flow?.forgetOidc();
    },
  });
</script>

<div class="flex flex-col gap-6" data-part="identity">
  <ChoiceCards
    label={t('setupWizard.identity.title')}
    value={draft.mode}
    {options}
    onchange={(mode: SetupMode) => {
      draft.mode = mode;
    }}
  />
  {#if draft.mode === 'required'}
    <Card>
      <CardContent class="flex flex-col gap-4">
        <Field
          label={t('setupWizard.identity.username')}
          error={showErrors ? errors.username : ''}
          control={Input}
          controlProps={{
            value: draft.username,
            type: 'text',
            autocomplete: 'username',
            oninput: (event: Event & { currentTarget: HTMLInputElement }) => {
              draft.username = event.currentTarget.value;
            },
          }}
        />
        <Field
          label={t('setupWizard.identity.password')}
          error={showErrors ? errors.password : ''}
          control={Input}
          controlProps={{
            value: draft.password,
            type: 'password',
            autocomplete: 'new-password',
            oninput: (event: Event & { currentTarget: HTMLInputElement }) => {
              draft.password = event.currentTarget.value;
            },
          }}
        />
        <Field
          label={t('setupWizard.identity.confirm')}
          error={confirmError}
          control={Input}
          controlProps={{
            value: draft.confirmation,
            type: 'password',
            autocomplete: 'new-password',
            oninput: (event: Event & { currentTarget: HTMLInputElement }) => {
              draft.confirmation = event.currentTarget.value;
            },
          }}
        />
      </CardContent>
    </Card>
  {:else if draft.mode === 'oidc'}
    <Card data-part="identity-oidc">
      <CardContent class="flex flex-col gap-4">
        >
        <Field
          label={t('setupWizard.oidc.issuer')}
          description={t('setupWizard.oidc.issuerHint')}
          error={showErrors ? ssoErrors.issuer : ''}
          control={Input}
          controlProps={ssoField(
            'issuer',
            'https://sso.example.com/realms/home',
          )}
        />
        <Field
          label={t('setupWizard.oidc.clientId')}
          description={t('setupWizard.oidc.clientIdHint')}
          error={showErrors ? ssoErrors.clientId : ''}
          control={Input}
          controlProps={ssoField('clientId')}
        />
        <Field
          label={t('setupWizard.oidc.clientSecret')}
          description={t('setupWizard.oidc.clientSecretHint')}
          control={Input}
          controlProps={{
            value: draft.oidcSecret,
            type: 'password',
            autocomplete: 'off',
            oninput: (event: Event & { currentTarget: HTMLInputElement }) => {
              draft.oidcSecret = event.currentTarget.value;
              flow?.forgetOidc();
            },
          }}
        />
        <Field
          label={t('setupWizard.oidc.redirectUri')}
          description={t('setupWizard.oidc.redirectUriHint')}
          error={showErrors ? ssoErrors.redirectUri : ''}
          control={Input}
          controlProps={ssoField('redirectUri', redirectFallback)}
        />
        <Field
          label={t('setupWizard.oidc.subject')}
          description={t('setupWizard.oidc.subjectHint')}
          error={showErrors ? ssoErrors.subject : ''}
          control={Input}
          controlProps={ssoField('subject')}
        />
        {#if flow !== undefined}
          <div class="flex flex-col gap-3" data-part="identity-oidc-try">
            <div class="flex flex-wrap items-center gap-3">
              <Button
                variant="secondary"
                disabled={!triable || flow.trying}
                onclick={tryProvider}
              >
                {#if flow.trying}
                  <Spinner />
                {:else}
                  <PlugZapIcon aria-hidden="true" />
                {/if}
                {t('setupWizard.oidc.try')}
              </Button>
              <p class="text-sm text-muted-foreground">
                {t('setupWizard.oidc.tryHint')}
              </p>
            </div>
            {#if tryFault !== undefined}
              <FaultAlert fault={tryFault} />
            {/if}
            {#if tried !== undefined}
              <ul class="flex flex-col gap-2 text-sm" aria-live="polite">
                {#each triedSteps as { id, step } (id)}
                  {@const Icon = STEP_ICON[step.status]}
                  <li class="flex gap-2" data-status={step.status}>
                    <Icon
                      class={`mt-0.5 size-4 shrink-0 ${STEP_TONE[step.status]}`}
                      aria-hidden="true"
                    />
                    <div class="flex min-w-0 flex-col gap-0.5">
                      <span>
                        {STEP_TEXT[id][step.status]}
                      </span>
                      {#if step.detail !== undefined && step.detail !== ''}
                        <span
                          class="font-mono text-xs break-words text-muted-foreground"
                        >
                          {step.detail}
                        </span>
                      {/if}
                    </div>
                  </li>
                {/each}
              </ul>
            {/if}
          </div>
        {/if}
      </CardContent>
    </Card>
  {/if}
  <!-- Not a choice of its own: a sign-in system other than OpenID Connect is
       an extension, so the step points at the guide for writing one. -->
  <p class="text-sm text-muted-foreground" data-part="identity-custom">
    {t('setupWizard.identity.custom.note')}
    {#if docsHref !== undefined}
      {@const docs = docsHref}
      <Button variant="link">
        {#snippet child({ props }: { props: Record<string, unknown> })}
          <a
            {...props}
            href={`${docs}human-auth/#writing-an-identity-provider-plugin`}
            target="_blank"
            rel="noopener noreferrer"
          >
            {t('setupWizard.identity.custom.link')}
          </a>
        {/snippet}
      </Button>
    {/if}
  </p>
</div>
