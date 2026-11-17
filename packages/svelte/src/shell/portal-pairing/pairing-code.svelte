<!--
  The code a tester confirms on the Beta Portal, the button that opens the
  portal with the code already filled in, and the plain address to type it at
  when the window does not open.
-->
<script lang="ts">
  import ExternalLinkIcon from '@lucide/svelte/icons/external-link';

  import { formatDuration, t } from '../../i18n';
  import { Button, CopyButton } from '../../primitives';

  interface Props {
    code: string;
    /** The portal's page with the code filled in, or the plain page. */
    href: string;
    /** The plain page, where the code is typed by hand. */
    address: string | undefined;
    /** Seconds left, absent when the server named no expiry. */
    remaining: number | undefined;
    /** Draws the code smaller once the person has opened the portal. */
    compact?: boolean;
    openLabel: string;
    onopen: () => void;
  }

  let {
    code,
    href,
    address,
    remaining,
    compact = false,
    openLabel,
    onopen,
  }: Props = $props();

  const labelId = $props.id();
  const left = $derived(
    remaining === undefined ? undefined : formatDuration(remaining),
  );
</script>

<div class="flex flex-col gap-3">
  <div class="flex flex-col gap-1">
    <span id={labelId} class="text-xs text-muted-foreground">
      {t('portalPairing.code.label')}
    </span>
    <div class="flex items-center gap-2">
      <output
        aria-labelledby={labelId}
        data-slot="pairing-code"
        class={[
          'font-mono font-semibold tracking-widest',
          compact ? 'text-xl' : 'text-3xl',
        ]}
      >
        {code}
      </output>
      <CopyButton value={code} label={t('portalPairing.code.copy')} />
    </div>
  </div>

  <div>
    <Button variant={compact ? 'secondary' : 'default'}>
      {#snippet child({ props }: { props: Record<string, unknown> })}
        <a
          {...props}
          {href}
          target="_blank"
          rel="noopener noreferrer"
          onclick={onopen}
        >
          {openLabel}
          <ExternalLinkIcon aria-hidden="true" />
          <span class="sr-only">({t('portalPairing.code.newWindow')})</span>
        </a>
      {/snippet}
    </Button>
  </div>

  {#if address !== undefined}
    <p class="text-xs text-muted-foreground">
      {t('portalPairing.code.fallback', { address })}
    </p>
  {/if}
  {#if remaining !== undefined}
    <p class="text-xs text-muted-foreground">
      {left === ''
        ? t('portalPairing.code.expiring')
        : t('portalPairing.code.expires', { time: left ?? '' })}
    </p>
  {/if}
</div>
