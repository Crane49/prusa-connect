<!--
  The device-header slot: "Set as current" under a printer's header, above its
  tabs. The host hands it the printer's id and draws no frame
  around it (ADR-20261112-62y).
-->
<script lang="ts">
  import { heldSession } from '../../session';
  import { SetAsCurrent } from '../set-as-current';

  let { deviceId }: { deviceId: string } = $props();

  const { host } = heldSession();
</script>

<SetAsCurrent {deviceId} t={host.t} look="header" />
