<!--
  The device-row slot: "Set as current" at the end of each printer in every view
  of the listing. The host hands it the printer's id and draws no frame
  around it (ADR-20261112-62y).
-->
<script lang="ts">
  import { heldSession } from '../../session';
  import { SetAsCurrent } from '../set-as-current';

  let { deviceId }: { deviceId: string } = $props();

  const { host } = heldSession();
</script>

<SetAsCurrent {deviceId} t={host.t} look="row" />
