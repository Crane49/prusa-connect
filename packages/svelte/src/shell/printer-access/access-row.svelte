<!--
  One person on one printer: the role select and the access switch, or the
  sentence that says why there is nothing to change. Both access lists draw
  this row, so the printer page and the person page say the same thing.

  The switch draws what the server holds and both controls wait while a
  change is in flight, as the extension switch does.
-->
<script lang="ts">
  import {
    ACCESS_CHOICES,
    type AccessChoice,
    type AccessLock,
    type ChoiceBlock,
    type DeviceRole,
  } from '../../domain/printer-access';
  import { t } from '../../i18n';
  import {
    Badge,
    DataRow,
    type DataRowValue,
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
    Switch,
  } from '../../primitives';
  import { orgRoleLabel } from './presentation';
  import type { AccessRowProps } from './types';

  let { row, onOn, onChoice }: AccessRowProps = $props();

  const orgRole = $derived(orgRoleLabel(row.orgRole));

  // Written out, so the catalogue gate sees every key this row asks for.
  const CHOICE_KEY: Record<DeviceRole, string> = {
    viewer: 'printerAccess.choice.viewer',
    operator: 'printerAccess.choice.operator',
    manager: 'printerAccess.choice.manager',
  };
  const BLOCK_KEY: Record<ChoiceBlock, string> = {
    inOrg: 'printerAccess.because.inOrg',
    aboveCaller: 'printerAccess.because.aboveCaller',
    noOrgRole: 'printerAccess.because.noOrgRole',
  };
  const LOCK_KEY: Record<AccessLock, string> = {
    orgAdmin: 'printerAccess.lock.orgAdmin',
    self: 'printerAccess.lock.self',
    owner: 'printerAccess.lock.owner',
  };

  const labelOf = (choice: AccessChoice): string =>
    choice === 'org'
      ? t('printerAccess.choice.org', { role: orgRole })
      : t(CHOICE_KEY[choice]);

  const items = $derived(
    row.state.kind === 'editable'
      ? row.state.options.map((option) => ({
          value: option.value,
          label: labelOf(option.value),
          disabled: option.blocked !== undefined,
          reason:
            option.blocked === undefined
              ? undefined
              : t(BLOCK_KEY[option.blocked]),
        }))
      : ACCESS_CHOICES.map((value) => ({
          value,
          label: labelOf(value),
          disabled: true,
          reason: undefined,
        })),
  );

  const choose = (next: string): void => {
    if (row.state.kind !== 'editable' || next === row.state.choice) return;
    // The items are ACCESS_CHOICES, so the value is one of them.
    onChoice(row, next as AccessChoice);
  };

  const flip = (next: boolean): void => {
    onOn(row, next);
  };

  // A sentence, so it is drawn in the text face beside the mono facts.
  const note = $derived.by((): string | undefined => {
    const state = row.state;
    if (state.kind === 'locked') return t(LOCK_KEY[state.lock]);
    if (state.switchBlocked !== undefined) {
      return t('printerAccess.switchAboveCaller');
    }
    if (state.on && state.inert) {
      return t('printerAccess.inert', { role: orgRole });
    }

    return undefined;
  });

  const secondaries = $derived<DataRowValue[]>([
    ...row.details,
    note !== undefined && sentence,
  ]);
</script>

{#snippet sentence()}
  <span class="font-sans">{note}</span>
{/snippet}

<!-- On a narrow row the controls wrap under the name instead of squeezing it
     to a few letters: the name keeps 10rem and the controls move to the end
     of their own line. -->
<DataRow
  class="flex-wrap [&>:first-child]:min-w-40 [&>:last-child]:ms-auto"
  primary={row.primary}
  mono={row.mono}
  wrapValues
  secondaryValues={secondaries}
  aria-busy={row.busy}
  data-part="access-row"
>
  {#snippet actions()}
    {#if row.state.kind === 'locked'}
      {#if row.state.lock === 'orgAdmin'}
        <Badge variant="info">{orgRole}</Badge>
      {:else if row.state.lock === 'owner'}
        <Badge variant="outline">{t('printerAccess.lock.owner')}</Badge>
      {/if}
    {:else}
      {@const state = row.state}
      <div class="flex items-center gap-3">
        <Select
          value={state.choice}
          {items}
          disabled={row.busy || !state.on}
          onValueChange={choose}
        >
          <SelectTrigger
            size="sm"
            aria-label={t('printerAccess.role', { name: row.name })}
          >
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {#each items as item (item.value)}
              <SelectItem
                value={item.value}
                label={item.label}
                disabled={item.disabled}
              >
                {item.label}
                {#if item.reason !== undefined}
                  <span class="text-xs text-muted-foreground">
                    {item.reason}
                  </span>
                {/if}
              </SelectItem>
            {/each}
          </SelectContent>
        </Select>
        <Switch
          size="sm"
          aria-label={t('printerAccess.switch', { name: row.name })}
          bind:checked={() => state.on, flip}
          disabled={row.busy || (state.on && state.switchBlocked !== undefined)}
        />
      </div>
    {/if}
  {/snippet}
</DataRow>
