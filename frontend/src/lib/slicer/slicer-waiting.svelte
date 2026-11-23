<!--
  What PrusaSlicer shows while it signs the person in. Inside the slicer the
  sign-in form is never the way in: the slicer opens a page session and sets
  its cookie, and the page draws once the core accepts it (ADR-20261112-nay).
  When the slicer answers a refusal with nothing this server takes, the page
  says to sign in again from the slicer, and stops the spinner.
-->
<script lang="ts">
  import { translateFor } from '@prusa/connect-ui/i18n';
  import { EmptyState, Spinner } from '@prusa/connect-ui/primitives';

  import { APP_NAMESPACE } from '../i18n';

  let { stale = false }: { stale?: boolean } = $props();

  const t = translateFor(APP_NAMESPACE);
</script>

{#if stale}
  <div data-part="slicer-stale">
    <EmptyState
      headingLevel="h1"
      heading={t('app.slicer.stale.heading')}
      description={t('app.slicer.stale.note')}
    />
  </div>
{:else}
  <div data-part="slicer-waiting" role="status" aria-busy="true">
    <EmptyState
      headingLevel="h1"
      heading={t('app.slicer.waiting.heading')}
      description={t('app.slicer.waiting.note')}
    >
      {#snippet action()}
        <Spinner size="lg" tone="muted" />
      {/snippet}
    </EmptyState>
  </div>
{/if}
