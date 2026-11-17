<!--
  Whether a download is checked against a SHA-256 its source published. A
  local binary was never downloaded, so it has nothing to check.
-->
<script lang="ts">
  import { Badge } from '@prusa/connect-ui/primitives';
  import ShieldAlertIcon from '@lucide/svelte/icons/shield-alert';
  import ShieldCheckIcon from '@lucide/svelte/icons/shield-check';

  import type { Translate } from '../../api';
  import type { DigestState } from '../../releases';

  let { digest, t }: { digest: DigestState; t: Translate } = $props();
</script>

{#if digest === 'verified'}
  <Badge variant="outline" data-digest="verified">
    <ShieldCheckIcon class="size-3" aria-hidden="true" />{t(
      'slicer.digest.verified',
    )}
  </Badge>
{:else if digest === 'unverified'}
  <Badge variant="warning" data-digest="unverified">
    <ShieldAlertIcon class="size-3" aria-hidden="true" />{t(
      'slicer.digest.unverified',
    )}
  </Badge>
{:else}
  <span class="text-sm text-muted-foreground" data-digest="none"
    >{t('slicer.digest.none')}</span
  >
{/if}
