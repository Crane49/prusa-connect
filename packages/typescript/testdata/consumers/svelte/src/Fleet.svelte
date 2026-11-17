<script lang="ts">
  // One component against this server: the fleet as a list, and the stream
  // appended to as it arrives. The lane builds and type-checks it; it is not
  // run in a browser there.
  import { onMount } from "svelte";
  import { ApiError, PrusaConnect, type StreamEvent } from "@prusa/connect";

  let { url, auth }: { url: string; auth: string } = $props();

  let devices = $state<string[]>([]);
  let events = $state<string[]>([]);
  let failure = $state<string | undefined>(undefined);

  onMount(() => {
    const local = new PrusaConnect({ url, auth });

    void local
      .listDevices({ query: { limit: 100 } })
      .then((page) => {
        devices = (page.items ?? []).map((device) => `${device.id} ${device.model} connected=${String(device.connected)}`);
      })
      .catch((err: unknown) => {
        failure = err instanceof ApiError ? `${err.status} ${err.code ?? ""} ${err.message}` : String(err);
      });

    const subscription = local.events({ types: ["com.prusa.connect.device.presence.changed"] });

    void (async () => {
      for await (const event of subscription) {
        const seen: StreamEvent = event;
        events = [...events, `${seen.type} ${seen.id ?? ""}`];
      }
    })();

    return () => subscription.close();
  });
</script>

<h1>Printers</h1>
{#if failure !== undefined}
  <p>{failure}</p>
{/if}
<ul>
  {#each devices as device (device)}
    <li>{device}</li>
  {/each}
</ul>

<h2>Events</h2>
<ol>
  {#each events as event, index (index)}
    <li>{event}</li>
  {/each}
</ol>
