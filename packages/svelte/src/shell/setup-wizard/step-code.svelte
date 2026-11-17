<!-- The setup code the server shows, checked by reading the configuration. -->
<script lang="ts">
  import TerminalIcon from '@lucide/svelte/icons/square-terminal';

  import { describeFault, t } from '../../i18n';
  import { FaultAlert, Field, Input } from '../../primitives';
  import type { SetupDraft } from './setup-draft.svelte';

  interface StepCodeProps {
    draft: SetupDraft;
    error: Error | undefined;
    showErrors: boolean;
  }

  let { draft, error, showErrors }: StepCodeProps = $props();

  const missing = $derived(
    showErrors && draft.code.trim() === '' ? t('setupWizard.code.missing') : '',
  );
  const fault = $derived(
    error === undefined ? undefined : describeFault(error),
  );
</script>

<div class="flex flex-col gap-5" data-part="code">
  <div class="flex gap-3 rounded-md bg-muted p-4 text-sm text-muted-foreground">
    <TerminalIcon class="mt-0.5 size-5 shrink-0" aria-hidden="true" />
    <p>{t('setupWizard.code.where')}</p>
  </div>
  {#if fault !== undefined}
    <FaultAlert {fault} />
  {/if}
  <Field
    label={t('setupWizard.code.label')}
    error={missing}
    control={Input}
    controlProps={{
      value: draft.code,
      type: 'text',
      class: 'font-mono tracking-widest uppercase',
      autocomplete: 'one-time-code',
      autocapitalize: 'characters',
      spellcheck: false,
      oninput: (event: Event & { currentTarget: HTMLInputElement }) => {
        draft.code = event.currentTarget.value;
      },
    }}
  />
</div>
