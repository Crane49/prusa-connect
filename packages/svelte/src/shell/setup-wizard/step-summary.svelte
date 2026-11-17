<!-- Every answer on one page, each with the way back to the step that asked it. -->
<script lang="ts">
  import type { SetupConfiguration, SetupKey } from '../../data';
  import { describeFault, t } from '../../i18n';
  import {
    Alert,
    AlertDescription,
    AlertTitle,
    Button,
    Card,
    CardContent,
    FaultAlert,
  } from '../../primitives';
  import { labelOf } from './labels';
  import {
    type Change,
    changesOf,
    type KeyGroup,
    sectionOf,
    startOf,
    type StepId,
  } from './model';
  import type { SetupDraft } from './setup-draft.svelte';

  interface StepSummaryProps {
    draft: SetupDraft;
    contract: SetupConfiguration;
    /** Why the last confirm was refused. */
    error: Error | undefined;
    /** The start that failed on the last confirmed answers. */
    failure: string | undefined;
    /** The address the server answers on after the restart, when it moves. */
    moves: string | undefined;
    /** The address printers are given, when the encryption step names it. */
    printerAddress: string | undefined;
    /** Whether the answers turn the web app off. */
    webOff: boolean;
    /** Keys the operator's file sets to another value, which it keeps. */
    kept: SetupKey[];
    /** Groups the file and the answers set apart, which block the confirm. */
    blocked: KeyGroup[];
    onedit: (step: StepId) => void;
  }

  let {
    draft,
    contract,
    error,
    failure,
    moves,
    printerAddress,
    webOff,
    kept,
    blocked,
    onedit,
  }: StepSummaryProps = $props();

  // Why a group stops the confirm, and the step that settles it.
  const GROUP_TEXT: Readonly<Record<KeyGroup, string>> = {
    identity: 'setupWizard.summary.fileWinsIdentity',
    encryption: 'setupWizard.summary.fileWinsEncryption',
  };

  const changes = $derived(changesOf(contract, draft.answers, draft.mode));
  // A file that declares the connectors decides this, not the draft.
  const printersEncrypted = $derived(
    contract.printers ? draft.encryption.printers : contract.printers_tls,
  );
  const fault = $derived(
    error === undefined ? undefined : describeFault(error),
  );

  const valueOf = (change: Change): string => {
    if (typeof change.answer === 'boolean') {
      return change.answer
        ? t('setupWizard.field.on')
        : t('setupWizard.field.off');
    }
    if (change.key.secret) return '••••••••';

    return change.answer.replaceAll('\n', ', ');
  };

  const historyName = (retention: string): string => {
    if (retention === '48h') return t('setupWizard.history.days2');
    if (retention === '720h') return t('setupWizard.history.days30');
    if (retention === '0') return t('setupWizard.history.forever');

    return t('setupWizard.history.days7');
  };

  const rows = $derived([
    {
      label: t('setupWizard.summary.access'),
      value: t(`setupWizard.identity.${draft.mode}.title`),
      step: 'identity' as const,
    },
    ...(draft.mode === 'oidc'
      ? [
          {
            label: t('setupWizard.summary.issuer'),
            value: draft.oidc.issuer.trim(),
            step: 'identity' as const,
          },
          {
            label: t('setupWizard.oidc.clientSecret'),
            value:
              draft.oidcSecret === ''
                ? t('setupWizard.oidc.publicClient')
                : '••••••••',
            step: 'identity' as const,
          },
          {
            label: t('setupWizard.summary.adminSubject'),
            value: draft.oidc.subject.trim(),
            step: 'identity' as const,
          },
        ]
      : []),
    ...(draft.mode === 'required'
      ? [
          {
            label: t('setupWizard.summary.admin'),
            value: draft.username,
            step: 'identity' as const,
          },
        ]
      : []),
    {
      label: t('setupWizard.summary.admission'),
      value:
        draft.admission === 'ask'
          ? t('setupWizard.admission.ask.title')
          : t('setupWizard.admission.open.title'),
      step: 'admission' as const,
    },
    {
      label: t('setupWizard.summary.storage'),
      value: contract.storage.declared
        ? t('setupWizard.summary.storageDeclared')
        : (draft.storageRoot ?? contract.storage.root),
      step: contract.storage.declared ? undefined : ('storage' as const),
    },
    {
      label: t('setupWizard.history.label'),
      value: draft.historyOn
        ? historyName(draft.historyRetention)
        : t('setupWizard.summary.encryptNone'),
      step: 'storage' as const,
    },
    {
      label: t('setupWizard.summary.encryption'),
      value:
        [
          draft.encryption.web ? t('setupWizard.summary.encryptWeb') : '',
          printersEncrypted ? t('setupWizard.summary.encryptPrinters') : '',
        ]
          .filter((part) => part !== '')
          .join(', ') || t('setupWizard.summary.encryptNone'),
      step: 'encryption' as const,
    },
    ...(printerAddress === undefined
      ? []
      : [
          {
            label: t('setupWizard.summary.printerAddress'),
            value: printerAddress,
            step: 'encryption' as const,
          },
        ]),
    ...(draft.mqttOn
      ? [
          {
            label: t('setupWizard.mqtt.label'),
            value: draft.mqttUrl,
            step: 'section:network' as const,
          },
        ]
      : []),
  ]);
</script>

{#snippet row(
  label: string,
  value: string,
  step: StepId | undefined,
  mono: boolean,
)}
  <div class="flex items-center justify-between gap-4 py-3">
    <div class="flex min-w-0 flex-col gap-0.5">
      <dt class="text-sm text-muted-foreground">{label}</dt>
      <dd class={['truncate font-medium', mono && 'font-mono text-sm']}>
        {value}
      </dd>
    </div>
    {#if step !== undefined}
      <Button variant="ghost" size="sm" onclick={() => onedit(step)}>
        {t('setupWizard.summary.change')}
      </Button>
    {/if}
  </div>
{/snippet}

<div class="flex flex-col gap-6" data-part="summary">
  {#if failure !== undefined}
    <Alert variant="destructive">
      <AlertTitle>{t('setupWizard.failed.title')}</AlertTitle>
      <AlertDescription>
        <p>{t('setupWizard.failed.lead')}</p>
        <p class="mt-2 font-mono text-xs">{failure}</p>
      </AlertDescription>
    </Alert>
  {/if}
  {#if fault !== undefined}
    <FaultAlert {fault} />
  {/if}
  <Card>
    <CardContent>
      <dl class="divide-y divide-border">
        {#each rows as item (item.label)}
          {@render row(
            item.label,
            item.value,
            item.step,
            item.step === 'storage' || item.step === 'section:network',
          )}
        {/each}
      </dl>
    </CardContent>
  </Card>
  <section
    class="flex flex-col gap-2"
    aria-label={t('setupWizard.summary.settings')}
  >
    <h2 class="text-sm font-medium">{t('setupWizard.summary.settings')}</h2>
    {#if changes.length === 0}
      <p class="text-sm text-muted-foreground">
        {t('setupWizard.summary.unchanged')}
      </p>
    {:else}
      <Card>
        <CardContent>
          <dl class="divide-y divide-border">
            {#each changes as change (change.key.path)}
              {@render row(
                labelOf(change.key),
                valueOf(change),
                `section:${sectionOf(change.key)}`,
                true,
              )}
            {/each}
          </dl>
        </CardContent>
      </Card>
      <p class="text-sm text-muted-foreground">
        {t('setupWizard.summary.others')}
      </p>
    {/if}
  </section>
  {#if kept.length > 0}
    <Alert
      variant={blocked.length > 0 ? 'destructive' : 'warning'}
      data-part="file-wins"
    >
      <AlertTitle>{t('setupWizard.summary.fileWinsTitle')}</AlertTitle>
      <AlertDescription>
        <p>
          {t('setupWizard.summary.fileWins', {
            file: contract.config_file.path,
          })}
        </p>
        <ul class="mt-2 list-disc pl-5">
          {#each kept as key (key.path)}
            <li>
              {labelOf(key)}{#if !key.secret && key.value !== undefined}:
                <code>{String(startOf(key))}</code>{/if}
            </li>
          {/each}
        </ul>
        {#each blocked as group (group)}
          <p class="mt-2" data-part="file-blocks">{t(GROUP_TEXT[group])}</p>
        {/each}
      </AlertDescription>
    </Alert>
  {/if}
  {#if webOff}
    <Alert variant="warning" data-part="web-off">
      <AlertTitle>{t('setupWizard.summary.webOffTitle')}</AlertTitle>
      <AlertDescription>{t('setupWizard.summary.webOff')}</AlertDescription>
    </Alert>
  {/if}
  {#if moves !== undefined}
    <Alert variant="info">
      <AlertDescription
        >{t('setupWizard.summary.moves', { address: moves })}</AlertDescription
      >
    </Alert>
  {/if}
</div>
