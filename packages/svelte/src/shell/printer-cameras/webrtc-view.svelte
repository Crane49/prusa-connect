<!--
  One camera's H.264 live view, which the server relays over WebRTC. It
  connects when it is drawn and hangs up when it goes, so a live view runs
  only while somebody watches it.

  The negotiation is one request: the offer goes once every candidate of this
  browser is in it, and the answer carries every candidate of the server.
-->
<script lang="ts">
  import { onMount } from 'svelte';

  import type { CameraWatch } from './types';

  let {
    camera,
    label,
    watch,
    onFail,
  }: {
    camera: string;
    /** What a screen reader says the video is. */
    label: string;
    watch: CameraWatch;
    /** Called once when the live view could not start. */
    onFail: () => void;
  } = $props();

  let video = $state<HTMLVideoElement | undefined>(undefined);

  // How long the browser may take to find its own candidates. On a network
  // with no ICE server it finds its host candidates in well under a second,
  // and an offer sent without some of them still connects on the others.
  const GATHER_MS = 2000;

  const gathered = (connection: RTCPeerConnection): Promise<void> =>
    new Promise((resolve) => {
      if (connection.iceGatheringState === 'complete') {
        resolve();

        return;
      }

      const timer = setTimeout(resolve, GATHER_MS);
      connection.addEventListener('icegatheringstatechange', () => {
        if (connection.iceGatheringState === 'complete') {
          clearTimeout(timer);
          resolve();
        }
      });
    });

  onMount(() => {
    const ended = new AbortController();
    const connection = new RTCPeerConnection();
    connection.addTransceiver('video', { direction: 'recvonly' });
    connection.addEventListener('track', (event) => {
      if (video !== undefined) {
        video.srcObject = event.streams[0] ?? new MediaStream([event.track]);
      }
    });
    connection.addEventListener('connectionstatechange', () => {
      if (connection.connectionState === 'failed' && !ended.signal.aborted) {
        onFail();
      }
    });

    const start = async (): Promise<void> => {
      await connection.setLocalDescription(await connection.createOffer());
      await gathered(connection);
      const offer = connection.localDescription?.sdp ?? '';
      const answer = await watch(camera, offer, ended.signal);
      await connection.setRemoteDescription({ type: 'answer', sdp: answer });
    };

    start().catch(() => {
      if (!ended.signal.aborted) onFail();
    });

    return () => {
      ended.abort();
      connection.close();
    };
  });
</script>

<video
  bind:this={video}
  class="absolute inset-0 size-full object-contain"
  aria-label={label}
  autoplay
  muted
  playsinline
></video>
