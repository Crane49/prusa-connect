<!--
  The live view of a Buddy3D camera, drawn over its picture while a person
  plays it. The video is a WebRTC connection straight from the camera to this
  browser. The module only carries the offer and the answer, so the video
  never passes through the server.
-->
<script lang="ts">
  import { onDestroy, onMount } from 'svelte';

  import { said } from './api';
  import { held } from './held';

  let { camera }: { camera: string } = $props();

  const { store, t } = held();

  /** How long the browser gathers its own candidates before it answers anyway. */
  const GATHER_MS = 3000;

  let video: HTMLVideoElement | undefined = $state();
  let phase = $state<'starting' | 'playing' | 'failed'>('starting');
  // The module's own refusal, such as a stream switched off, said as it is.
  let refusal = $state<string | undefined>(undefined);

  let peer: RTCPeerConnection | undefined;
  let session: string | undefined;
  let closed = false;
  // A function, because the page may close during any await.
  const ended = (): boolean => closed;

  // The camera's candidates are in its offer and the route answers once, so
  // the answer waits until the browser has every candidate of its own.
  const gathered = (pc: RTCPeerConnection): Promise<void> =>
    new Promise((resolve) => {
      if (pc.iceGatheringState === 'complete') {
        resolve();

        return;
      }
      const timer = setTimeout(resolve, GATHER_MS);
      pc.addEventListener('icegatheringstatechange', () => {
        if (pc.iceGatheringState !== 'complete') return;
        clearTimeout(timer);
        resolve();
      });
    });

  const start = async (): Promise<void> => {
    const live = await store.startLive(camera);
    session = live.session;
    if (ended()) {
      void store.stopLive(camera, live.session);

      return;
    }

    // On the same network as the camera, its host candidates reach it, so
    // no STUN or TURN server is named.
    const pc = new RTCPeerConnection();
    peer = pc;
    pc.ontrack = (event) => {
      if (video !== undefined) video.srcObject = event.streams[0] ?? new MediaStream([event.track]);
    };
    pc.onconnectionstatechange = () => {
      if (pc.connectionState === 'connected') phase = 'playing';
      if (pc.connectionState === 'failed') phase = 'failed';
    };
    // The firmware opens its ping-pong channel and answers on it by itself,
    // so it is taken and left alone.
    pc.ondatachannel = () => undefined;

    await pc.setRemoteDescription({ type: 'offer', sdp: live.offer });
    await pc.setLocalDescription(await pc.createAnswer());
    await gathered(pc);
    if (ended() || pc.localDescription === null) return;

    await store.answerLive(camera, live.session, pc.localDescription.sdp);
  };

  onMount(() => {
    start().catch((error: unknown) => {
      refusal = said(error);
      phase = 'failed';
    });
  });

  onDestroy(() => {
    closed = true;
    peer?.close();
    if (session !== undefined) void store.stopLive(camera, session);
  });
</script>

<div class="relative size-full bg-black" data-part="buddy3d-live">
  <video bind:this={video} class="size-full object-contain" autoplay muted playsinline></video>
  {#if phase !== 'playing'}
    <p class="absolute inset-0 flex items-center justify-center text-sm text-white">
      {phase === 'failed' ? (refusal ?? t('buddy3d.liveFailed')) : t('buddy3d.liveStarting')}
    </p>
  {/if}
</div>
