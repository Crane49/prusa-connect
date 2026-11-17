<!-- A device-card line 300 characters long, for the outlet's overflow rule. -->
<script lang="ts">
  let { deviceId }: { deviceId: string } = $props();
</script>

<span data-device={deviceId}>{'x'.repeat(300)}</span>
