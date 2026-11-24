<!--
  The Buddy3D section under one camera in a printer's Cameras tab. Under a
  camera this module pairs it shows how the Buddy3D camera is doing and, until
  the camera has used its token, how to set it up. While it is connected,
  three tabs hold its controls, its settings and what it reports about
  itself. Under a Buddy3D camera not paired yet it offers to pair one. Under
  any other camera it draws nothing.
-->
<script lang="ts">
  import {
    Alert,
    AlertDescription,
    Badge,
    Button,
    Checkbox,
    ConfirmDialog,
    Field,
    Input,
    Label,
    Tabs,
    TabsContent,
    TabsList,
    TabsTrigger,
  } from '@prusa/connect-ui/primitives';
  import { onDestroy } from 'svelte';

  import CameraControls from './camera-controls.svelte';
  import CameraInfo from './camera-info.svelte';
  import CameraSettings from './camera-settings.svelte';
  import { held } from './held';
  import { saveFile, sdcardCfg, tar } from './pairing';

  let { camera }: { camera: string } = $props();

  const { store, t } = held();
  onDestroy(store.watch());

  // A camera added a moment ago is not in the declarations read when the tab
  // opened, so they are read again for it.
  $effect(() => {
    if (!store.declares(camera)) void store.loadDeclared();
  });

  const paired = $derived(store.pairedAs(camera));

  $effect(() => {
    if (paired !== undefined) store.askOperate(camera);
  });

  // The controls are offered only to a person who may operate the camera,
  // while it is connected to take them.
  const controls = $derived(paired?.connected === true && store.mayOperate(camera));
  const restarts = $derived(controls && paired?.features?.includes('CameraReboot') === true);

  const offer = $derived(paired === undefined && store.mayConfigure && store.isMine(camera));
  const problem = $derived(store.problemFor === camera ? store.problem : '');

  let setWifi = $state<boolean>(false);
  let ssid = $state('');
  let password = $state('');
  let customTime = $state<boolean>(false);
  let timeServer = $state('');
  // The camera asks a time server on port 123 only, so a port is refused.
  const badTimeServer = $derived(customTime && (timeServer.trim() === '' || timeServer.includes(':')));

  const when = (value: string | undefined): string =>
    value === undefined ? t('buddy3d.never') : new Date(value).toLocaleString();

  // The camera reads everything from one local_cfg.tar on its SD card: where
  // to send, the certificate, the Wi-Fi and the token. The archive is made
  // here, so the Wi-Fi password never reaches the server. With no Wi-Fi or
  // time server named, the camera keeps its own.
  const saveLocalCfg = async (): Promise<void> => {
    if (paired?.token === undefined) return;
    const srvCfg = store.status?.srv_cfg ?? '';
    const archive = tar([
      {
        name: 'srv_cfg.txt',
        text: customTime ? `${srvCfg}\nNTP_SERVER=${timeServer.trim()}\n` : srvCfg,
      },
      { name: 'self-signed.pem', text: await store.certificate() },
      { name: 'sdcard_cfg.txt', text: setWifi ? sdcardCfg(ssid.trim(), password, paired.token) : sdcardCfg('', '', paired.token) },
    ]);
    saveFile('local_cfg.tar', archive, 'application/x-tar');
  };

  const text = (
    value: string,
    set: (next: string) => void,
    type = 'text',
  ): Record<string, unknown> => ({
    value,
    type,
    autocomplete: 'off',
    oninput: (event: Event & { currentTarget: HTMLInputElement }) => {
      set(event.currentTarget.value);
    },
  });
</script>

{#if paired !== undefined || offer}
  <div class="flex flex-col gap-3 border-t pt-4 text-sm" data-part="buddy3d">
    {#if problem !== ''}
      <Alert variant="destructive"><AlertDescription>{problem}</AlertDescription></Alert>
    {/if}

    {#if paired !== undefined}
      <div class="flex flex-wrap items-center gap-2">
        {#if paired.connected}
          <Badge variant="success">{t('buddy3d.connected')}</Badge>
        {:else if paired.bound}
          <Badge variant="secondary">{t('buddy3d.offline')}</Badge>
        {:else}
          <Badge variant="outline">{t('buddy3d.waiting')}</Badge>
        {/if}
        {#if paired.bound}
          <span class="text-muted-foreground">{t('buddy3d.lastFrame', { when: when(paired.last_frame) })}</span>
        {/if}
        <span class="ms-auto flex gap-2">
          <Button
            size="sm"
            variant="secondary"
            disabled={store.busy || !paired.connected}
            onclick={() => void store.snapshot(camera)}
          >
            {t('buddy3d.snapshot')}
          </Button>
          {#if restarts}
            <ConfirmDialog
              title={t('buddy3d.restartTitle')}
              description={t('buddy3d.restartSaid')}
              confirmLabel={t('buddy3d.restart')}
              variant="confirm"
              onConfirm={() => void store.control(camera, { restart: true })}
            >
              {#snippet trigger({ props })}
                <Button {...props} size="sm" variant="secondary" disabled={store.busy}>
                  {t('buddy3d.restart')}
                </Button>
              {/snippet}
            </ConfirmDialog>
          {/if}
          {#if store.mayConfigure}
            <Button size="sm" variant="ghost" disabled={store.busy} onclick={() => void store.forget(camera)}>
              {t('buddy3d.forget')}
            </Button>
          {/if}
        </span>
      </div>

      {#if paired.connected}
        <Tabs value="control">
          <TabsList>
            <TabsTrigger value="control">{t('buddy3d.tabControl')}</TabsTrigger>
            <TabsTrigger value="settings">{t('buddy3d.tabSettings')}</TabsTrigger>
            <TabsTrigger value="info">{t('buddy3d.tabInfo')}</TabsTrigger>
          </TabsList>
          <TabsContent value="control" class="pt-2">
            <CameraControls {paired} {controls} />
          </TabsContent>
          <TabsContent value="settings" class="pt-2">
            <CameraSettings {paired} {controls} />
          </TabsContent>
          <TabsContent value="info" class="pt-2">
            <CameraInfo {paired} {controls} />
          </TabsContent>
        </Tabs>
      {/if}

      {#if paired.problem}
        <p class="text-destructive">{paired.problem}</p>
      {/if}

      {#if !paired.bound && paired.token !== undefined}
        <section class="flex flex-col gap-3 rounded-md border p-4" data-part="setup">
          <h3 class="font-medium">{t('buddy3d.setupTitle', { name: camera })}</h3>
          <p>{t('buddy3d.instructions')}</p>
          <div class="flex flex-col gap-2">
            <Label class="flex w-fit items-center gap-2 font-normal">
              <Checkbox bind:checked={setWifi} />
              {t('buddy3d.setWifi')}
            </Label>
            {#if setWifi}
              <div class="grid gap-3 sm:grid-cols-2">
                <Field
                  label={t('buddy3d.ssid')}
                  control={Input}
                  controlProps={text(ssid, (next) => (ssid = next))}
                />
                <Field
                  label={t('buddy3d.password')}
                  control={Input}
                  controlProps={text(password, (next) => (password = next), 'password')}
                />
              </div>
            {/if}
          </div>
          <div class="flex flex-col gap-2">
            <Label class="flex w-fit items-center gap-2 font-normal">
              <Checkbox bind:checked={customTime} />
              {t('buddy3d.customTime')}
            </Label>
            {#if customTime}
              <Field
                class="max-w-xs"
                label={t('buddy3d.timeServer')}
                control={Input}
                description={t('buddy3d.timeServerSaid')}
                controlProps={{
                  ...text(timeServer, (next) => (timeServer = next)),
                  placeholder: '192.168.1.1',
                }}
              />
            {/if}
          </div>
          <div>
            <Button
              size="sm"
              disabled={(setWifi && ssid.trim() === '') || !store.status?.srv_cfg || badTimeServer}
              onclick={() => void saveLocalCfg()}
            >
              {t('buddy3d.saveLocalCfg')}
            </Button>
          </div>
          <p class="text-muted-foreground">
            {t('buddy3d.tokenShown', { token: paired.token })}
          </p>
        </section>
      {/if}
    {:else}
      <form
        class="flex flex-col gap-3"
        onsubmit={(event) => {
          event.preventDefault();
          void store.setUp(camera);
        }}
      >
        <p class="text-muted-foreground">{t('buddy3d.offer')}</p>
        <div>
          <Button type="submit" size="sm" disabled={store.busy}>
            {t('buddy3d.setUp')}
          </Button>
        </div>
      </form>
    {/if}
  </div>
{/if}
