<script lang="ts">
  import { describeFault, formatDate, t } from '../../i18n';
  import {
    Button,
    FaultAlert,
    Field,
    Input,
    Separator,
    SettingsPane,
    SettingsRow,
    Skeleton,
  } from '../../primitives';
  import type { AccountIdentity, SettingsAccountScreenProps } from './types';

  let {
    source,
    identity,
    identityLoading = false,
    changing = false,
    fault,
    changed = false,
    passwordless = false,
    onChangePassword,
  }: SettingsAccountScreenProps = $props();

  // What `PasswordBody` documents on `set-account-password`.
  const SHORTEST = 8;
  const LONGEST = 72;

  let current = $state('');
  let next = $state('');
  let confirm = $state('');

  const waiting = $derived(source.status === 'unknown' || identityLoading);
  const subject = $derived(source.principal?.id);

  // The roles pane's own words.
  const ROLES: Readonly<Record<string, string>> = {
    admin: 'settingsPeople.role.admin',
    operator: 'settingsPeople.role.operator',
    viewer: 'settingsPeople.role.viewer',
  };

  const added = $derived(
    identity?.createdAt === undefined
      ? undefined
      : formatDate(identity.createdAt),
  );

  const ROWS = [
    {
      id: 'name',
      label: () => t('settingsAccount.identity.name'),
      mono: false,
      value: (of?: AccountIdentity) => of?.displayName,
    },
    {
      id: 'username',
      label: () => t('settingsAccount.identity.username'),
      mono: true,
      value: (of?: AccountIdentity) => of?.username,
    },
    {
      id: 'role',
      label: () => t('settingsAccount.identity.role'),
      mono: false,
      value: () => roleText,
    },
    {
      id: 'added',
      label: () => t('settingsAccount.identity.added'),
      mono: false,
      value: () => added,
    },
  ] as const;

  const roleText = $derived.by(() => {
    const key = identity?.role === undefined ? undefined : ROLES[identity.role];

    // A role this build has no word for is printed as the core wrote it.
    return key === undefined ? identity?.role : t(key);
  });

  // `Field` takes a string, and an empty one is no error at all.
  const lengthError = $derived(
    next !== '' && (next.length < SHORTEST || next.length > LONGEST)
      ? t('settingsAccount.password.length')
      : '',
  );
  const mismatchError = $derived(
    confirm !== '' && confirm !== next
      ? t('settingsAccount.password.mismatch')
      : '',
  );

  const blocked = $derived(
    changing ||
      current === '' ||
      next === '' ||
      confirm === '' ||
      lengthError !== '' ||
      mismatchError !== '',
  );

  const refusal = $derived(
    fault === undefined ? undefined : describeFault(fault),
  );

  // A refusal keeps what was typed.
  $effect(() => {
    if (changed) {
      current = '';
      next = '';
      confirm = '';
    }
  });

  const submit = (event: SubmitEvent): void => {
    event.preventDefault();
    if (blocked) return;
    onChangePassword(current, next);
  };
</script>

<SettingsPane
  class="w-full"
  title={t('settingsAccount.title')}
  message={changed ? t('settingsAccount.password.changed') : undefined}
>
  <div class="flex flex-col gap-4 pb-6">
    <div class="flex flex-col gap-1">
      <h3 class="text-xs font-normal text-muted-foreground">
        {t('settingsAccount.identity.title')}
      </h3>
      <p class="text-sm text-muted-foreground">
        {t('settingsAccount.identity.note')}
      </p>
    </div>

    <SettingsRow label={t('settingsAccount.identity.subject')}>
      {#snippet control()}
        {#if waiting}
          <Skeleton
            class="h-5 w-40"
            role="status"
            aria-label={t('settingsAccount.loading')}
          />
        {:else if subject !== undefined}
          <span class="font-mono text-sm">{subject}</span>
        {/if}
      {/snippet}
    </SettingsRow>

    <!-- The four rows the record carries stand in their own shape while. -->
    {#each ROWS as row (row.id)}
      {#if identityLoading || row.value(identity) !== undefined}
        <SettingsRow
          label={row.label()}
          description={row.id === 'role'
            ? t('settingsAccount.identity.roleNote')
            : undefined}
        >
          {#snippet control()}
            {#if identityLoading}
              <Skeleton class="h-5 w-32" />
            {:else}
              <span class={['text-sm', row.mono && 'font-mono']}>
                {row.value(identity)}
              </span>
            {/if}
          {/snippet}
        </SettingsRow>
      {/if}
    {/each}
  </div>

  {#if !passwordless}
    <Separator />

    <!-- The submit sits in the flow under the fields. -->
    <form onsubmit={submit} class="flex flex-col gap-4 pt-6">
      <div class="flex flex-col gap-1">
        <h3 class="text-xs font-normal text-muted-foreground">
          {t('settingsAccount.password.title')}
        </h3>
        <p class="text-sm text-muted-foreground">
          {t('settingsAccount.password.note')}
        </p>
      </div>

      <FaultAlert fault={refusal} />

      <div class="grid gap-4 @[44rem]/pane:grid-cols-2">
        <Field
          class="max-w-sm @[44rem]/pane:col-span-2"
          label={t('settingsAccount.password.current')}
          control={Input}
          controlProps={{
            value: current,
            type: 'password',
            autocomplete: 'current-password',
            oninput: (event: Event & { currentTarget: HTMLInputElement }) => {
              current = event.currentTarget.value;
            },
          }}
        />
        <Field
          class="max-w-sm"
          label={t('settingsAccount.password.next')}
          error={lengthError}
          control={Input}
          controlProps={{
            value: next,
            type: 'password',
            autocomplete: 'new-password',
            oninput: (event: Event & { currentTarget: HTMLInputElement }) => {
              next = event.currentTarget.value;
            },
          }}
        />
        <Field
          class="max-w-sm"
          label={t('settingsAccount.password.confirm')}
          error={mismatchError}
          control={Input}
          controlProps={{
            value: confirm,
            type: 'password',
            autocomplete: 'new-password',
            oninput: (event: Event & { currentTarget: HTMLInputElement }) => {
              confirm = event.currentTarget.value;
            },
          }}
        />
      </div>

      <Button type="submit" class="self-start" disabled={blocked}>
        {t('settingsAccount.password.submit')}
      </Button>
    </form>
  {/if}
</SettingsPane>
