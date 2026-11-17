<!--
  Where a module's own database goes, picked before the install is approved
  (ADR-20261021-bqc rule 5): a SQLite file on this server, a Postgres database the
  person made, or a new database on this server's own Postgres. An update
  offers to keep the database the module has, and says where it is.
-->
<script lang="ts">
  import type { components } from '@prusa/connect';
  import { translateFor } from '@prusa/connect-ui/i18n';
  import {
    Input,
    Label,
    RadioGroup,
    RadioGroupItem,
  } from '@prusa/connect-ui/primitives';

  import { APP_NAMESPACE } from '../../i18n';
  import { databasePlace } from '../database-place';
  import { type DatabaseMode, isPostgresURL } from '../requests';

  let {
    mode,
    dsn = $bindable(''),
    update,
    current,
    onpick,
  }: {
    /** The place picked. */
    mode: DatabaseMode;
    /** Called with the place a person picks. */
    onpick: (next: DatabaseMode) => void;
    /** The connection string typed for a Postgres database the person made. */
    dsn?: string;
    /** Whether the module is installed, which offers to keep its database. */
    update: boolean;
    /** Where the installed module's database is now, when the server says. */
    current?: components['schemas']['ResourceDatabase'] | undefined;
  } = $props();

  const t = translateFor(APP_NAMESPACE);
  const id = $props.id();

  const modes = $derived<readonly DatabaseMode[]>([
    ...(update ? (['keep'] as const) : []),
    'sqlite',
    'postgres',
    'provision',
  ]);

  const labelOf = (each: DatabaseMode): string => {
    switch (each) {
      case 'keep':
        return t('app.settings.extensions.confirm.database.keep');
      case 'sqlite':
        return t('app.settings.extensions.confirm.database.sqlite');
      case 'postgres':
        return t('app.settings.extensions.confirm.database.postgres');
      case 'provision':
        return t('app.settings.extensions.confirm.database.provision');
    }
  };

  const dsnValid = $derived(isPostgresURL(dsn));
  const kept = $derived(databasePlace(current, t));
</script>

<div class="flex flex-col gap-2" data-part="database">
  <span id={`${id}-legend`} class="font-medium">
    {t('app.settings.extensions.confirm.database')}
  </span>
  <RadioGroup
    class="gap-2"
    value={mode}
    aria-labelledby={`${id}-legend`}
    onValueChange={(next: string) => {
      const picked = modes.find((each) => each === next);
      if (picked !== undefined) onpick(picked);
    }}
  >
    {#each modes as each (each)}
      <div class="flex items-start gap-2">
        <RadioGroupItem value={each} id={`${id}-${each}`} class="mt-0.5" />
        <div class="flex flex-col gap-0.5">
          <Label for={`${id}-${each}`}>{labelOf(each)}</Label>
          {#if each === 'keep' && kept !== ''}
            <span class="text-xs text-muted-foreground">{kept}</span>
          {:else if each === 'provision'}
            <span class="text-xs text-muted-foreground">
              {t('app.settings.extensions.confirm.database.provisionHelp')}
            </span>
          {/if}
        </div>
      </div>
    {/each}
  </RadioGroup>

  {#if mode === 'postgres'}
    <div class="flex flex-col gap-1 ps-6" data-part="database-dsn">
      <Label for={`${id}-dsn`} variant="hint">
        {t('app.settings.extensions.confirm.database.dsn')}
      </Label>
      <Input
        id={`${id}-dsn`}
        type="password"
        autocomplete="off"
        spellcheck="false"
        size="sm"
        value={dsn}
        oninput={(event: Event) => {
          dsn = (event.currentTarget as HTMLInputElement).value;
        }}
        aria-invalid={!dsnValid}
      />
      {#if !dsnValid}
        <span class="text-xs text-destructive" role="status">
          {t('app.settings.extensions.confirm.database.dsnInvalid')}
        </span>
      {/if}
    </div>
  {/if}
</div>
