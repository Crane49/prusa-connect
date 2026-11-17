<!--
  Step one: the file that tells a printer where this server is, and how it
  reaches the printer. The download is the step's one action. The text of the
  file and the values inside it are behind one disclosure, for a person who
  checks them or types them on the printer instead.
-->
<script lang="ts">
  import BookOpenIcon from '@lucide/svelte/icons/book-open';
  import DownloadIcon from '@lucide/svelte/icons/download';
  import TriangleAlertIcon from '@lucide/svelte/icons/triangle-alert';

  import { isOwnAddress } from '../../domain/printer-address';
  import { t } from '../../i18n';
  import {
    Alert,
    AlertDescription,
    AlertTitle,
    Button,
    CodeBlock,
    Collapsible,
    CollapsibleContent,
    CollapsibleTrigger,
    CopyField,
  } from '../../primitives';
  import NumberedStep from './numbered-step.svelte';
  import { buildIni } from './presentation';
  import type { AddPrinterScreenProps, AutoConnect } from './types';

  let {
    connection,
    authority,
    docsHref,
  }: {
    connection: AutoConnect;
    authority?: AddPrinterScreenProps['authority'];
    docsHref?: string | undefined;
  } = $props();

  const CERT_FILE = 'connect.der';

  let valuesOpen = $state(false);

  const ini = $derived(buildIni(connection));
  const file = $derived(t('addPrinter.iniFile'));
  const customCert = $derived(connection.tls && connection.customCert === true);
  // A data URL, as the code block's own download uses: a few lines of text
  // hold no object that has to be revoked when the address changes.
  const href = $derived(
    `data:text/plain;charset=utf-8,${encodeURIComponent(ini)}`,
  );

  let authorityHref = $state<string>();

  $effect(() => {
    if (!authority) {
      authorityHref = undefined;

      return;
    }
    const url = URL.createObjectURL(authority);
    authorityHref = url;

    return () => {
      URL.revokeObjectURL(url);
    };
  });
</script>

<div class="flex flex-col gap-6" data-part="step-prepare">
  {#if isOwnAddress(connection.host)}
    <Alert variant="warning" data-part="loopback-host">
      <TriangleAlertIcon aria-hidden="true" />
      <AlertTitle>
        {t('addPrinter.loopback.title', { host: connection.host })}
      </AlertTitle>
      <AlertDescription>
        {t('addPrinter.loopback.note', { host: connection.host })}
      </AlertDescription>
    </Alert>
  {/if}

  <!-- The one thing this step asks for, so it is the primary button. -->
  <div>
    <!-- The label names a long file, so on a narrow phone it is cut short
         rather than pushing the page sideways. -->
    <Button size="lg" class="max-w-full">
      {#snippet child({ props })}
        <a {...props} {href} download={file}>
          <DownloadIcon aria-hidden="true" />
          <span class="truncate"
            >{t('addPrinter.prepare.download.label', { file })}</span
          >
        </a>
      {/snippet}
    </Button>
  </div>

  <ol class="flex flex-col gap-3">
    <NumberedStep number={1}>
      <p>{t('addPrinter.prepare.copy')}</p>
    </NumberedStep>
    {#if customCert}
      <NumberedStep number={2} part="custom-cert">
        {#if authority !== null}
          <p class="font-medium">{t('addPrinter.prepare.customCert')}</p>
          {#if authorityHref !== undefined}
            <Button variant="secondary" class="mt-2 self-start">
              {#snippet child({ props })}
                <a
                  {...props}
                  href={authorityHref}
                  download={CERT_FILE}
                  data-part="authority-download"
                >
                  <DownloadIcon aria-hidden="true" />
                  {t('addPrinter.prepare.download.label', { file: CERT_FILE })}
                </a>
              {/snippet}
            </Button>
          {/if}
        {:else}
          <div data-part="authority-missing" class="flex flex-col gap-2">
            <p class="font-medium">
              {t('addPrinter.prepare.customCert.missing')}
            </p>
            {#if docsHref !== undefined}
              {@const docs = docsHref}
              <Button variant="link" size="inline-icon" class="self-start">
                {#snippet child({ props })}
                  <a
                    {...props}
                    href={`${docs}certificates/`}
                    target="_blank"
                    rel="noopener noreferrer"
                  >
                    <BookOpenIcon aria-hidden="true" />
                    {t('addPrinter.prepare.customCert.docs')}
                  </a>
                {/snippet}
              </Button>
            {/if}
          </div>
        {/if}
      </NumberedStep>
    {/if}
    <NumberedStep number={customCert ? 3 : 2}>
      <p>{t('addPrinter.prepare.plug')}</p>
    </NumberedStep>
  </ol>

  <p class="text-sm text-muted-foreground">
    {t('addPrinter.prepare.address', { host: connection.host })}
  </p>

  <!-- The file itself and the values in it are for a person who checks
       them or types them on the printer, so they stay shut until asked. -->
  <Collapsible bind:open={valuesOpen} data-part="file-values">
    <CollapsibleTrigger variant="inline">
      {t('addPrinter.values.heading')}
    </CollapsibleTrigger>
    <CollapsibleContent>
      <div class="@container/values flex flex-col gap-4 pt-3">
        <CodeBlock label={file} value={ini} heightClass="h-24" />
        <p class="text-sm text-muted-foreground">
          {t('addPrinter.values.note')}
        </p>
        <div class="grid grid-cols-1 gap-4 @[32rem]/values:grid-cols-2">
          <CopyField
            label={t('addPrinter.values.hostname')}
            value={connection.host}
          />
          <!-- The port is a wire value a person copies into a printer. -->
          <CopyField
            label={t('addPrinter.values.port')}
            value={String(connection.port)}
          />
          <CopyField
            label={t('addPrinter.values.tls')}
            value={connection.tls
              ? t('addPrinter.values.on')
              : t('addPrinter.values.off')}
          />
          <CopyField
            label={t('addPrinter.values.wsUrl')}
            value={connection.wsUrl}
          />
          <CopyField
            label={t('addPrinter.values.registerUrl')}
            value={connection.registerUrl}
          />
        </div>
      </div>
    </CollapsibleContent>
  </Collapsible>
</div>
