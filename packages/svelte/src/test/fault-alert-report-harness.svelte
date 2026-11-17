<!-- A test host: a FaultAlert under an application shell that set the link. -->
<script lang="ts">
  import FaultAlert from '../primitives/fault-alert/fault-alert.svelte';
  import { setReportHref } from '../primitives/fault-alert/report';
  import type { FaultAlertProps } from '../primitives/fault-alert/types';

  let props: FaultAlertProps = $props();

  setReportHref('/settings/diagnostics');
</script>

<FaultAlert {...props} />
