<!--
  The Diagnostics room: a scrubbed file for the support team. The server does
  the scrubbing. The screen needs the person's consent, chooses whether module
  output and the files extensions offer go in, and hands the result to the
  caller to save, or to send to the support portal. A refusal, such as a viewer who is not an admin, is shown by
  its text.
-->
<script lang="ts">
  import { formatList, t } from '../../i18n';
  import {
    Button,
    Checkbox,
    CopyButton,
    FaultAlert,
    RadioGroup,
    Label,
    RadioGroupItem,
    SettingsPane,
  } from '../../primitives';
  import { describeSendFailure } from './send-failure';
  import type { SendIntent, SettingsDiagnosticsScreenProps } from './types';

  let {
    onDownload,
    onCopy,
    onSend,
    extensions = [],
  }: SettingsDiagnosticsScreenProps = $props();

  const INTENTS: readonly { value: SendIntent; key: string }[] = [
    { value: 'new', key: 'settingsDiagnostics.intent.new.label' },
    { value: 'existing', key: 'settingsDiagnostics.intent.existing.label' },
  ];

  // Every key is written out rather than assembled from a value.
  const PARTS = [
    'settingsDiagnostics.part.logs',
    'settingsDiagnostics.part.crash',
    'settingsDiagnostics.part.config',
    'settingsDiagnostics.part.modules',
    'settingsDiagnostics.part.printers',
    'settingsDiagnostics.part.browser',
  ];

  let modules = $state(false);
  // On by default: an extension that offers files was installed to collect
  // the evidence a report needs, and the person reads the zip before sending.
  let fromExtensions = $state(true);
  const offered = $derived(extensions.length > 0);
  const choice = $derived({
    modules,
    extensions: offered && fromExtensions,
  });
  let consent = $state(false);
  let intent = $state<SendIntent>('new');
  let busy = $state(false);
  let failure = $state<unknown>();
  const fault = $derived(
    failure === undefined ? undefined : describeSendFailure(failure),
  );

  const run = async (action: () => Promise<void>): Promise<void> => {
    busy = true;
    failure = undefined;
    try {
      await action();
    } catch (thrown) {
      failure = thrown;
    } finally {
      busy = false;
    }
  };

  const chooseIntent = (value: string): void => {
    if (value === 'new' || value === 'existing') intent = value;
  };

  const modulesId = $props.id();
  const consentId = `${modulesId}-consent`;
  const extensionsId = `${modulesId}-extensions`;
  const intentId = `${modulesId}-intent`;
</script>

<SettingsPane
  class="w-full"
  title={t('settingsDiagnostics.title')}
  description={t('settingsDiagnostics.intro')}
>
  <div class="flex flex-col gap-4 pb-6">
    <ul class="flex list-disc flex-col gap-1 pl-5 text-sm">
      {#each PARTS as key (key)}
        <li>{t(key)}</li>
      {/each}
    </ul>

    <div class="flex items-start gap-2">
      <Checkbox id={modulesId} bind:checked={modules} class="mt-0.5" />
      <div class="flex flex-col gap-0.5">
        <Label for={modulesId}>
          {t('settingsDiagnostics.modules')}
        </Label>
        <span class="text-xs text-muted-foreground">
          {t('settingsDiagnostics.modulesHelp')}
        </span>
      </div>
    </div>

    {#if offered}
      <div class="flex items-start gap-2" data-part="extensions">
        <Checkbox
          id={extensionsId}
          bind:checked={fromExtensions}
          class="mt-0.5"
        />
        <div class="flex flex-col gap-0.5">
          <Label for={extensionsId}>
            {t('settingsDiagnostics.extensions', {
              names: formatList(extensions),
            })}
          </Label>
          <span class="text-xs text-muted-foreground">
            {t('settingsDiagnostics.extensionsHelp')}
          </span>
        </div>
      </div>
    {/if}

    <div class="flex items-start gap-2">
      <Checkbox id={consentId} bind:checked={consent} class="mt-0.5" />
      <Label for={consentId}>
        {t('settingsDiagnostics.consent')}
      </Label>
    </div>

    {#if onSend}
      <div class="flex flex-col gap-2">
        <span id={intentId}>
          {t('settingsDiagnostics.intent.legend')}
        </span>
        <RadioGroup
          class="gap-2"
          value={intent}
          onValueChange={chooseIntent}
          aria-labelledby={intentId}
        >
          {#each INTENTS as each (each.value)}
            <div class="flex items-center gap-2">
              <RadioGroupItem
                value={each.value}
                id={`${intentId}-${each.value}`}
              />
              <Label for={`${intentId}-${each.value}`}>
                {t(each.key)}
              </Label>
            </div>
          {/each}
        </RadioGroup>
        <span class="text-xs text-muted-foreground">
          {t('settingsDiagnostics.sendWhere')}
        </span>
      </div>
    {/if}

    <FaultAlert {fault} />

    <div class="flex flex-wrap items-center gap-2">
      <Button
        disabled={busy || !consent}
        aria-busy={busy}
        onclick={() => {
          void run(() => onDownload(choice));
        }}
      >
        {t('settingsDiagnostics.download')}
      </Button>
      {#if onSend}
        <Button
          variant="secondary"
          disabled={busy || !consent}
          aria-busy={busy}
          onclick={() => {
            // Called in the click itself, so the caller can open the portal's
            // tab before the first await and no popup blocker stops it.
            void run(() => onSend(choice, intent));
          }}
        >
          {t('settingsDiagnostics.send')}
        </Button>
      {/if}
      <CopyButton value={onCopy} label={t('settingsDiagnostics.copy')} />
    </div>

    <p class="text-xs text-muted-foreground">
      {t('settingsDiagnostics.hint')}
    </p>
  </div>
</SettingsPane>
