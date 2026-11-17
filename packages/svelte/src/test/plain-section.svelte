<!-- A device-card section that works, for the slot tests. -->
<script lang="ts">
  let { deviceId }: { deviceId: string } = $props();
</script>

<p data-testid="plain-section">plain section for {deviceId}</p>
