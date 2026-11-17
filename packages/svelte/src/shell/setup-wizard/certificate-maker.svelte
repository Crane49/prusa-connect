<!--
  Makes the authority and both certificates on the server, for the names the
  person keeps or adds, and hands the authority over for the printer's USB
  stick and for a browser to trust.
-->
<script lang="ts">
  import DownloadIcon from '@lucide/svelte/icons/download';
  import KeyRoundIcon from '@lucide/svelte/icons/key-round';

  import type { SetupCertificates } from '../../data';
  import { describeFault, t } from '../../i18n';
  import {
    Button,
    Card,
    CardContent,
    FaultAlert,
    Field,
    Textarea,
  } from '../../primitives';

  interface CertificateMakerProps {
    names: string;
    busy: boolean;
    made: SetupCertificates | undefined;
    /** The paths the server made in an earlier visit, after a reload. */
    madeBefore: boolean;
    error: Error | undefined;
    onnames: (names: string) => void;
    onmake: () => void;
  }

  let {
    names,
    busy,
    made,
    madeBefore,
    error,
    onnames,
    onmake,
  }: CertificateMakerProps = $props();

  const fault = $derived(
    error === undefined ? undefined : describeFault(error),
  );

  // The authority comes in base64 inside the answer; a browser saves it as a
  // file without asking the server again.
  const download = (filename: string): void => {
    if (made === undefined) return;
    const bytes = Uint8Array.from(atob(made.printer_authority), (c) =>
      c.charCodeAt(0),
    );
    const url = URL.createObjectURL(
      new Blob([bytes], { type: 'application/pkix-cert' }),
    );
    const link = document.createElement('a');
    link.href = url;
    link.download = filename;
    link.click();
    URL.revokeObjectURL(url);
  };
</script>

<Card data-part="certificate-maker">
  <CardContent class="flex flex-col gap-4">
    <Field
      label={t('setupWizard.encryption.names')}
      description={t('setupWizard.encryption.namesHint')}
      control={Textarea}
      controlProps={{
        value: names,
        rows: 4,
        class: 'font-mono',
        spellcheck: false,
        oninput: (event: Event & { currentTarget: HTMLTextAreaElement }) => {
          onnames(event.currentTarget.value);
        },
      }}
    />
    <div>
      <Button variant="secondary" disabled={busy} onclick={onmake}>
        <KeyRoundIcon aria-hidden="true" />
        {made === undefined && !madeBefore
          ? t('setupWizard.encryption.make')
          : t('setupWizard.encryption.remake')}
      </Button>
    </div>
    {#if fault !== undefined}
      <FaultAlert {fault} />
    {/if}
    {#if made !== undefined}
      <div class="flex flex-col gap-2 text-sm" data-part="certificates-made">
        <p class="text-success-text">
          {t('setupWizard.encryption.made', { folder: made.folder })}
        </p>
        <div class="flex flex-wrap gap-2">
          <Button
            variant="secondary"
            size="sm"
            onclick={() => download('connect.der')}
          >
            <DownloadIcon aria-hidden="true" />
            {t('setupWizard.encryption.downloadPrinter')}
          </Button>
          <Button
            variant="ghost"
            size="sm"
            onclick={() => download('prusa-connect-ca.cer')}
          >
            <DownloadIcon aria-hidden="true" />
            {t('setupWizard.encryption.downloadBrowser')}
          </Button>
        </div>
      </div>
    {:else if madeBefore}
      <p class="text-sm text-success-text">
        {t('setupWizard.encryption.madeBefore')}
      </p>
    {/if}
  </CardContent>
</Card>
