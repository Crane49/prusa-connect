<!--
  What sits beside a camera's picture in the Cameras tab: what the camera
  serves and where it is sent its pictures. A person who may change a generic
  or MJPEG camera also changes the addresses it is read from here. A module's
  camera gets its own section below.
-->
<script lang="ts">
  import {
    type Camera,
    cameraTypeOf,
    type CameraDeclaration,
    type CameraDraft,
    printerOf,
  } from '../../data';
  import { t, type FaultText } from '../../i18n';
  import {
    Button,
    FaultAlert,
    Field,
    Input,
    KeyValueList,
    type KeyValueRow,
    Tabs,
    TabsContent,
    TabsList,
    TabsTrigger,
  } from '../../primitives';

  let {
    camera,
    declared,
    deviceId,
    pushUrl,
    writable,
    busy,
    fault,
    onSave,
  }: {
    camera: Camera;
    /** The camera's settings, when the person may read them. */
    declared: CameraDeclaration | undefined;
    /** The printer on screen. */
    deviceId: string;
    /** The address a camera that takes pushes is sent its pictures at. */
    pushUrl: string | undefined;
    /** Whether the person may change this camera. */
    writable: boolean;
    busy: boolean;
    /** Why the last save of this camera was refused. */
    fault: FaultText | undefined;
    onSave: (draft: CameraDraft) => void;
  } = $props();

  // A setting the server holds as anything but text fills nothing.
  const textOf = (value: unknown): string =>
    typeof value === 'string' ? value : '';

  // What is typed, until the camera is read again after a save.
  let snapshotUrl = $derived(textOf(declared?.config.snapshot_url));
  let streamUrl = $derived(textOf(declared?.config.stream_url));

  const type = $derived(declared === undefined ? '' : cameraTypeOf(declared));
  // A generic camera needs no address: with none it takes the pictures the
  // camera sends. An MJPEG camera reads at least one.
  const takesUrls = $derived(type === 'generic' || type === 'mjpeg');
  const needsUrl = $derived(type === 'mjpeg');

  const changed = $derived(
    declared !== undefined &&
      (snapshotUrl.trim() !== textOf(declared.config.snapshot_url) ||
        streamUrl.trim() !== textOf(declared.config.stream_url)),
  );
  const missingUrl = $derived(
    needsUrl && snapshotUrl.trim() === '' && streamUrl.trim() === '',
  );

  const submit = (event: SubmitEvent): void => {
    event.preventDefault();
    if (declared === undefined || !changed || missingUrl || busy) return;
    onSave({
      name: declared.name,
      type: declared.type,
      printer: printerOf(declared) ?? deviceId,
      snapshotUrl,
      streamUrl,
    });
  };

  const urlProps = (
    value: string,
    set: (next: string) => void,
  ): Record<string, unknown> => ({
    value,
    type: 'url',
    autocomplete: 'off',
    disabled: busy,
    oninput: (event: Event & { currentTarget: HTMLInputElement }) => {
      set(event.currentTarget.value);
    },
  });

  const liveOf = (row: Camera): string => {
    if (row.stream && row.watch) return t('printerCameras.info.liveBoth');
    if (row.stream) return t('printerCameras.info.liveMjpeg');
    if (row.watch) return t('printerCameras.info.liveH264');

    return t('printerCameras.info.none');
  };

  // What the server says of a camera it does not run is not known, so a
  // camera that is not running shows none of it.
  const rows = $derived<KeyValueRow[]>([
    ...(camera.serving
      ? [
          {
            id: 'still',
            label: t('printerCameras.info.still'),
            value: camera.snapshot
              ? t('printerCameras.info.available')
              : t('printerCameras.info.none'),
          },
          {
            id: 'live',
            label: t('printerCameras.info.live'),
            value: liveOf(camera),
          },
          {
            id: 'formats',
            label: t('printerCameras.info.formats'),
            value: camera.formats?.length
              ? camera.formats.join(', ')
              : undefined,
            mono: true,
          },
          {
            id: 'pace',
            label: t('printerCameras.info.pace'),
            value:
              camera.min_interval_ms === undefined
                ? undefined
                : t('printerCameras.info.seconds', {
                    seconds: camera.min_interval_ms / 1000,
                  }),
          },
        ]
      : []),
    ...(pushUrl === undefined
      ? []
      : [
          {
            id: 'upload',
            label: t('printerCameras.info.upload'),
            value: pushUrl,
            mono: true,
            wrap: true,
            copyable: true,
          },
        ]),
  ]);
</script>

{#snippet info()}
  <KeyValueList {rows} />
{/snippet}

{#if writable && takesUrls}
  <Tabs value="settings">
    <TabsList>
      <TabsTrigger value="settings">
        {t('printerCameras.tabSettings')}
      </TabsTrigger>
      {#if rows.length > 0}
        <TabsTrigger value="info">{t('printerCameras.tabInfo')}</TabsTrigger>
      {/if}
    </TabsList>
    <TabsContent value="settings" class="pt-2">
      <form class="flex flex-col gap-4" data-part="settings" onsubmit={submit}>
        {#if fault}
          <FaultAlert {fault} />
        {/if}
        <Field
          label={t('printerCameras.form.snapshotUrl')}
          {...needsUrl
            ? {}
            : { description: t('printerCameras.form.snapshotUrlNote') }}
          error={changed && missingUrl
            ? t('printerCameras.form.missingUrl')
            : ''}
          control={Input}
          controlProps={urlProps(snapshotUrl, (next) => {
            snapshotUrl = next;
          })}
        />
        <Field
          label={t('printerCameras.form.streamUrl')}
          {...needsUrl
            ? {}
            : { description: t('printerCameras.form.streamUrlNote') }}
          control={Input}
          controlProps={urlProps(streamUrl, (next) => {
            streamUrl = next;
          })}
        />
        <div>
          <Button
            type="submit"
            size="sm"
            disabled={busy || !changed || missingUrl}
            aria-busy={busy}
          >
            {t('printerCameras.form.save')}
          </Button>
        </div>
      </form>
    </TabsContent>
    <TabsContent value="info" class="pt-2">
      {@render info()}
    </TabsContent>
  </Tabs>
{:else if rows.length > 0}
  {@render info()}
{/if}
