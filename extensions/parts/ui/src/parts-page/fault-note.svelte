<!--
  What the catalogue refused, by its code.

  The catalogue line for the code is the heading, the server's English detail
  is under it, and the code itself is beside them. A person quotes the code in
  a bug report and a reader in another language still gets a sentence
  (frontend-i18n.md rule 5).

  A change somebody else made first is the one refusal a person acts on from
  here, so it carries the button that reads the catalogue again. It draws
  nothing when nothing was refused, so the page can leave it in the markup.
-->
<script lang="ts">
  import { Button, FaultAlert } from '@prusa/connect-ui/primitives';

  import { faultText } from '../faults';
  import { heldSession } from '../session';

  let {
    fault,
    onDismiss,
  }: { fault: unknown; onDismiss?: (() => void) | undefined } = $props();

  const { host, catalogue } = heldSession();
  const t = host.t;

  /** The refusal a person answers by reading again. */
  const CHANGED = 'parts/CHANGED';

  const shown = $derived(fault === undefined ? undefined : faultText(fault, t));
</script>

{#if shown}
  <FaultAlert fault={shown}>
    {#snippet actions()}
      {#if shown.code === CHANGED}
        <Button
          size="sm"
          variant="secondary"
          onclick={() => {
            onDismiss?.();
            void catalogue.refresh();
          }}
        >
          {t('parts.fault.reload')}
        </Button>
      {/if}
      {#if onDismiss}
        <Button size="sm" variant="ghost" onclick={onDismiss}>
          {t('parts.fault.dismiss')}
        </Button>
      {/if}
    {/snippet}
  </FaultAlert>
{/if}
