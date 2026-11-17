<script lang="ts">
  import { formatList, t } from '../../../i18n';
  import { Button, ConfirmDialog, Field, Input } from '../../../primitives';
  import type { CommandRowProps } from './types';
  import {
    descriptionKeyOf,
    haltsTheMachine,
    labelKeyOf,
    spellOut,
  } from './vocabulary';

  let {
    entry,
    projection,
    marked,
    sending,
    pending,
    readingDrive = false,
    onSend,
  }: CommandRowProps = $props();

  let draft = $state<Record<string, string>>({});

  const refusal = $derived(
    projection.kind === 'disabled' ? projection.reason : undefined,
  );

  // The refusal is a catalogue key and the states are a list.
  const reason = $derived(
    refusal === undefined
      ? undefined
      : t(
          refusal.key,
          refusal.states === undefined
            ? undefined
            : { states: formatList([...refusal.states]) },
        ),
  );

  const blocked = $derived(projection.kind !== 'available' || sending);

  // An argument the printer expects has to be filled in before the send.
  const asks = $derived(entry.destructive || entry.args.length > 0);

  const name = $derived.by(() => {
    const key = labelKeyOf(entry.verb);

    return key === undefined ? spellOut(entry.verb) : t(key);
  });

  const description = $derived.by(() => {
    const key = descriptionKeyOf(entry.verb);

    return key === undefined ? undefined : t(key);
  });

  // The colour warns about the machine stopping where it stands.
  const variant = $derived(
    haltsTheMachine(entry.verb) ? 'destructive' : 'secondary',
  );

  // The button's ink is the generic "Run" on every row.
  const label = $derived(t('printerDetailCommands.runFor', { verb: name }));

  const write = (argument: string) => (event: Event) => {
    const field = event.currentTarget;
    if (field instanceof HTMLInputElement) draft[argument] = field.value;
  };

  const send = () => {
    onSend(entry.verb, entry.args.length === 0 ? undefined : { ...draft });
  };
</script>

<!-- The id is the verb, so another surface can link straight to one row. -->
<div
  id="command-{entry.verb}"
  data-slot="command-row"
  tabindex="-1"
  aria-current={marked ? true : undefined}
  class={[
    'flex flex-col gap-2 border-b border-border px-2 py-3 transition-shadow last:border-b-0 @md/commands:flex-row @md/commands:items-start @md/commands:justify-between @md/commands:gap-4',
    marked && 'ring-2 ring-ring ring-inset',
  ]}
>
  <div class="flex min-w-0 flex-col gap-1 @md/commands:flex-1">
    <p class="text-sm font-medium text-foreground">{name}</p>
    {#if description}
      <p class="text-xs text-muted-foreground">{description}</p>
    {/if}
    {#if reason}
      <!-- Muted and not `--warning-text`. -->
      <p class="text-xs text-muted-foreground">{reason}</p>
    {/if}
  </div>
  <div class="flex shrink-0 flex-col items-end gap-2">
    {#if blocked || !asks}
      <Button
        size="lg"
        {variant}
        disabled={blocked}
        pending={pending === entry.verb}
        aria-label={label}
        onclick={blocked ? undefined : send}
      >
        {t('printerDetailCommands.run')}
      </Button>
    {:else}
      <ConfirmDialog
        trigger={triggerButton}
        title={t('printerDetailCommands.confirm.title', { verb: name })}
        description={entry.destructive
          ? t('printerDetailCommands.confirm.destructive')
          : t('printerDetailCommands.confirm.arguments')}
        confirmLabel={t('printerDetailCommands.run')}
        onConfirm={send}
      >
        {#each entry.args as argument (argument)}
          <Field
            label={argument}
            control={Input}
            controlProps={{
              value: draft[argument] ?? '',
              oninput: write(argument),
              class: 'font-mono',
            }}
          />
        {/each}
      </ConfirmDialog>
    {/if}
    {#if readingDrive && pending === entry.verb}
      <p class="text-xs text-muted-foreground">
        {t('printerDetail.readingDriveHint')}
      </p>
    {/if}
  </div>
</div>

{#snippet triggerButton({ props }: { props: Record<string, unknown> })}
  <Button
    {...props}
    size="lg"
    {variant}
    pending={pending === entry.verb}
    aria-label={label}
  >
    {t('printerDetailCommands.run')}
  </Button>
{/snippet}
