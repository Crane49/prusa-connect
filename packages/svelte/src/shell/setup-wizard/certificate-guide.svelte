<!--
  How to make a certificate by hand, for a person who keeps their own
  authority. The commands are the ones the repository's own script runs, and
  the printer's limits are spelled out: ECDSA, and an address written twice.
-->
<script lang="ts">
  import BookOpenIcon from '@lucide/svelte/icons/book-open';

  import { t } from '../../i18n';
  import {
    Button,
    CodeBlock,
    Collapsible,
    CollapsibleContent,
    CollapsibleTrigger,
  } from '../../primitives';

  let { host, docsHref }: { host: string; docsHref: string | undefined } =
    $props();

  const name = $derived(host.trim() === '' ? 'printserver.local' : host.trim());
  const isAddress = $derived(/^[\d.]+$/.test(name));
  const san = $derived(isAddress ? `IP:${name},DNS:${name}` : `DNS:${name}`);

  // Shell commands are not text: they read the same in every language.
  const commands = $derived(
    [
      'mkdir -p certs && cd certs',
      'openssl ecparam -name prime256v1 -genkey -noout -out ca.key',
      'openssl req -x509 -new -sha256 -key ca.key -out ca.crt -days 3650 \\',
      '  -subj "/O=Prusa Connect/CN=Prusa Connect Root CA" \\',
      '  -addext "basicConstraints=critical,CA:TRUE,pathlen:0" \\',
      '  -addext "keyUsage=critical,keyCertSign,cRLSign"',
      'openssl ecparam -name prime256v1 -genkey -noout -out server.key',
      `openssl req -new -sha256 -key server.key -out server.csr -subj "/CN=${name}"`,
      'openssl x509 -req -sha256 -in server.csr -CA ca.crt -CAkey ca.key -CAcreateserial \\',
      '  -out server.crt -days 825 -extfile <(printf "%s\\n" \\',
      '  "basicConstraints=critical,CA:FALSE" "keyUsage=critical,digitalSignature" \\',
      `  "extendedKeyUsage=serverAuth" "subjectAltName=${san}")`,
      'openssl x509 -in ca.crt -outform DER -out connect.der',
    ].join('\n'),
  );
</script>

<Collapsible>
  <CollapsibleTrigger variant="inline">
    {t('setupWizard.encryption.guide.title')}
  </CollapsibleTrigger>
  <CollapsibleContent>
    <div class="flex flex-col gap-3 pt-2 text-sm" data-part="certificate-guide">
      <p>{t('setupWizard.encryption.guide.lead')}</p>
      <CodeBlock
        label={t('setupWizard.encryption.guide.commands')}
        value={commands}
      />
      <ul class="flex list-disc flex-col gap-1 ps-5 text-muted-foreground">
        <li>{t('setupWizard.encryption.guide.ecdsa')}</li>
        <li>{t('setupWizard.encryption.guide.address')}</li>
        <li>{t('setupWizard.encryption.guide.files')}</li>
      </ul>
      {#if docsHref !== undefined}
        {@const docs = docsHref}
        <Button variant="link" size="inline-icon">
          {#snippet child({ props }: { props: Record<string, unknown> })}
            <a
              {...props}
              href={`${docs}certificates/`}
              target="_blank"
              rel="noopener noreferrer"
            >
              <BookOpenIcon class="size-4" aria-hidden="true" />
              {t('setupWizard.encryption.guide.more')}
            </a>
          {/snippet}
        </Button>
      {/if}
    </div>
  </CollapsibleContent>
</Collapsible>
