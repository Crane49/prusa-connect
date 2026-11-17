<!-- Where one certificate comes from: made by the server, or the person's files. -->
<script lang="ts">
  import { t } from '../../i18n';
  import { FaultAlert, Field, Input } from '../../primitives';
  import ChoiceCards from './choice-cards.svelte';
  import type { CertificateSource } from './setup-draft.svelte';

  interface CertificateSourceProps {
    label: string;
    source: CertificateSource;
    certificate: string;
    key: string;
    error: string;
    onsource: (source: CertificateSource) => void;
    oncertificate: (path: string) => void;
    onkey: (path: string) => void;
  }

  let {
    label,
    source,
    certificate,
    key,
    error,
    onsource,
    oncertificate,
    onkey,
  }: CertificateSourceProps = $props();

  const options = $derived([
    {
      value: 'generate' as const,
      title: t('setupWizard.encryption.generate.title'),
      note: t('setupWizard.encryption.generate.note'),
      recommended: true,
    },
    {
      value: 'own' as const,
      title: t('setupWizard.encryption.own.title'),
      note: t('setupWizard.encryption.own.note'),
    },
  ]);
</script>

<div class="flex flex-col gap-4">
  <ChoiceCards {label} value={source} {options} onchange={onsource} />
  {#if source === 'own'}
    <Field
      label={t('setupWizard.encryption.certificate')}
      control={Input}
      controlProps={{
        value: certificate,
        type: 'text',
        class: 'font-mono',
        spellcheck: false,
        placeholder: '/etc/prusa-connect/server.crt',
        oninput: (event: Event & { currentTarget: HTMLInputElement }) => {
          oncertificate(event.currentTarget.value);
        },
      }}
    />
    <Field
      label={t('setupWizard.encryption.key')}
      {error}
      control={Input}
      controlProps={{
        value: key,
        type: 'text',
        class: 'font-mono',
        spellcheck: false,
        placeholder: '/etc/prusa-connect/server.key',
        oninput: (event: Event & { currentTarget: HTMLInputElement }) => {
          onkey(event.currentTarget.value);
        },
      }}
    />
  {:else if error !== ''}
    <FaultAlert fault={{ code: undefined, title: error, detail: undefined }} />
  {/if}
</div>
