<!-- Creating the first administrator on a server that answers. -->
<script lang="ts">
  import { describeFault, t } from '../../i18n';
  import {
    Button,
    Card,
    CardContent,
    CardHeader,
    FaultAlert,
    Field,
    Input,
  } from '../../primitives';
  import type { FirstRunScreenProps } from './types';

  let { host, busy = false, error, onCreate }: FirstRunScreenProps = $props();

  // The core hashes with bcrypt, which takes 8 to 72 bytes.
  const MIN_PASSWORD = 8;

  let code = $state('');
  let username = $state('');
  let password = $state('');
  let confirmation = $state('');
  let submitted = $state(false);
  let confirmationTouched = $state(false);

  const codeError = $derived(
    submitted && code.trim() === '' ? t('firstRun.missingCode') : '',
  );
  const usernameError = $derived(
    submitted && username.trim() === '' ? t('firstRun.missingUsername') : '',
  );
  const passwordError = $derived(
    submitted && password.length < MIN_PASSWORD
      ? t('firstRun.passwordShort')
      : '',
  );
  // The mismatch shows while typing.
  const confirmationError = $derived(
    confirmationTouched && confirmation !== password
      ? t('firstRun.passwordsDiffer')
      : '',
  );

  const fault = $derived(
    error === undefined ? undefined : describeFault(error),
  );

  const submit = (event: SubmitEvent): void => {
    event.preventDefault();
    submitted = true;
    confirmationTouched = true;

    if (
      codeError !== '' ||
      usernameError !== '' ||
      passwordError !== '' ||
      confirmationError !== ''
    )
      return;

    onCreate(username.trim(), password, code.trim());
  };
</script>

<div
  class="flex min-h-svh items-center justify-center bg-background px-4 py-10"
>
  <div class="w-full max-w-sm">
    <Card>
      <CardHeader>
        <h1 class="text-xl leading-none font-semibold">
          {t('firstRun.title')}
        </h1>
        {#if host !== undefined && host !== ''}
          <p class="font-mono text-xs text-muted-foreground">{host}</p>
        {/if}
      </CardHeader>
      <CardContent class="flex flex-col gap-4">
        <p class="text-sm text-muted-foreground">{t('firstRun.note')}</p>
        {#if fault !== undefined}
          <FaultAlert {fault} />
        {/if}
        <form class="flex flex-col gap-4" onsubmit={submit}>
          <Field
            label={t('firstRun.code')}
            description={t('firstRun.codeWhere')}
            error={codeError}
            control={Input}
            controlProps={{
              value: code,
              type: 'text',
              autocomplete: 'one-time-code',
              autocapitalize: 'characters',
              spellcheck: false,
              oninput: (event: Event & { currentTarget: HTMLInputElement }) => {
                code = event.currentTarget.value;
              },
            }}
          />
          <Field
            label={t('firstRun.username')}
            error={usernameError}
            control={Input}
            controlProps={{
              value: username,
              type: 'text',
              autocomplete: 'username',
              oninput: (event: Event & { currentTarget: HTMLInputElement }) => {
                username = event.currentTarget.value;
              },
            }}
          />
          <Field
            label={t('firstRun.password')}
            error={passwordError}
            control={Input}
            controlProps={{
              value: password,
              type: 'password',
              autocomplete: 'new-password',
              oninput: (event: Event & { currentTarget: HTMLInputElement }) => {
                password = event.currentTarget.value;
              },
            }}
          />
          <Field
            label={t('firstRun.confirm')}
            error={confirmationError}
            control={Input}
            controlProps={{
              value: confirmation,
              type: 'password',
              autocomplete: 'new-password',
              oninput: (event: Event & { currentTarget: HTMLInputElement }) => {
                confirmation = event.currentTarget.value;
                confirmationTouched = true;
              },
            }}
          />
          <Button type="submit" class="w-full" disabled={busy}>
            {t('firstRun.submit')}
          </Button>
        </form>
      </CardContent>
    </Card>
  </div>
</div>
