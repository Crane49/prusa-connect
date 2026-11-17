<!--
  The dialog that adds a camera: its name, its type, the printer it watches
  and, for a generic or MJPEG camera, the two addresses it is read from. A
  camera that exists is changed beside its picture in the Cameras tab.

  The fields start empty each time it opens.
-->
<script lang="ts">
  import { type CameraDraft, type CameraTypeChoice } from '../../data';
  import { t, type FaultText } from '../../i18n';
  import {
    Button,
    Dialog,
    DialogClose,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
    FaultAlert,
    Field,
    Input,
    Label,
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
  } from '../../primitives';
  import { CAMERA_TYPE_KEYS, type CameraPrinterChoice } from './types';

  // A new camera is generic unless the server offers more: it takes the
  // pictures a camera sends, or fetches them, and may also stream. A camera of
  // another type, such as push or mjpeg, is listed and changed as it is.
  const GENERIC = 'generic';

  let {
    open,
    deviceId,
    printers,
    types,
    taken,
    busy,
    fault,
    onOpenChange,
    onSave,
  }: {
    open: boolean;
    /** The printer a new camera watches. */
    deviceId: string;
    printers: readonly CameraPrinterChoice[];
    /**
     * The types a new camera may be declared as, as the server listed them.
     * Empty offers the core's generic camera alone.
     */
    types: readonly CameraTypeChoice[];
    /** The camera names already declared, which a new camera may not take. */
    taken: readonly string[];
    busy: boolean;
    /** Why the last save was refused. */
    fault: FaultText | undefined;
    onOpenChange: (open: boolean) => void;
    onSave: (draft: CameraDraft) => void;
  } = $props();

  // The server's rule for an instance name, a DNS label of at most 63
  // characters, so a name it would refuse is refused here first.
  const NAME_PATTERN = /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/;

  const uid = $props.id();
  const typeId = `${uid}-type`;
  const printerId = `${uid}-printer`;

  let name = $state('');
  // The type a new camera is declared as, which the person picks when the app
  // offers more than one.
  let chosen = $state(GENERIC);
  let printer = $state('');
  let snapshotUrl = $state('');
  let streamUrl = $state('');
  let attempted = $state(false);
  // True from a send until the name is typed again.
  let sent = $state(false);

  // Emptied each time the dialog opens.
  $effect.pre(() => {
    if (!open) return;
    name = '';
    chosen = GENERIC;
    printer = deviceId;
    snapshotUrl = '';
    streamUrl = '';
    attempted = false;
    sent = false;
  });

  // A generic camera needs no address: with none it takes the images the
  // camera sends. An MJPEG camera reads at least one.
  const needsUrl = $derived(chosen === 'mjpeg');
  const takesUrls = $derived(chosen === 'generic' || chosen === 'mjpeg');

  // A core type is named in the reader's language and a module's type by the
  // title its module gave it.
  const typeOptions = $derived(
    (types.length > 0 ? types : [{ type: GENERIC }]).map(
      (row: CameraTypeChoice | { type: string; title?: undefined }) => ({
        value: row.type,
        label: row.title ?? t(CAMERA_TYPE_KEYS[row.type] ?? row.type),
      }),
    ),
  );

  const printerOptions = $derived(
    printers.map((row) => ({ value: row.id, label: row.label })),
  );

  const nameError = $derived.by(() => {
    const typed = name.trim();
    if (typed === '')
      return attempted ? t('printerCameras.form.missingName') : '';
    if (!NAME_PATTERN.test(typed)) return t('printerCameras.form.badName');
    // Once sent, the list may already hold the new name while the dialog
    // closes. A name somebody else took first comes back as the server's fault.
    if (!sent && taken.includes(typed)) {
      return t('printerCameras.form.takenName');
    }

    return '';
  });

  // An MJPEG camera with neither address declares nothing to show.
  const urlError = $derived(
    attempted &&
      needsUrl &&
      snapshotUrl.trim() === '' &&
      streamUrl.trim() === ''
      ? t('printerCameras.form.missingUrl')
      : '',
  );

  const blocked = $derived(
    busy ||
      nameError !== '' ||
      name.trim() === '' ||
      (needsUrl && snapshotUrl.trim() === '' && streamUrl.trim() === ''),
  );

  const submit = (event: SubmitEvent): void => {
    event.preventDefault();
    attempted = true;
    if (blocked) return;

    sent = true;
    onSave({
      name: name.trim(),
      type: chosen,
      printer,
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
    oninput: (event: Event & { currentTarget: HTMLInputElement }) => {
      set(event.currentTarget.value);
    },
  });
</script>

<Dialog {open} {onOpenChange}>
  <DialogContent>
    <form class="flex flex-col gap-4" onsubmit={submit}>
      <DialogHeader>
        <DialogTitle>{t('printerCameras.form.addTitle')}</DialogTitle>
        <DialogDescription>{t('printerCameras.form.note')}</DialogDescription>
      </DialogHeader>
      {#if fault}
        <FaultAlert {fault} />
      {/if}
      <Field
        label={t('printerCameras.form.name')}
        description={t('printerCameras.form.nameNote')}
        error={nameError}
        control={Input}
        controlProps={{
          value: name,
          type: 'text',
          autocomplete: 'off',
          oninput: (event: Event & { currentTarget: HTMLInputElement }) => {
            name = event.currentTarget.value;
            sent = false;
          },
        }}
      />
      {#if typeOptions.length > 1}
        <!-- Not a `Field`: a select's name belongs on its trigger. -->
        <div class="flex flex-col gap-2">
          <Label for={typeId}>{t('printerCameras.form.type')}</Label>
          <Select
            value={chosen}
            items={typeOptions}
            onValueChange={(next: string) => {
              chosen = next;
            }}
          >
            <SelectTrigger id={typeId}><SelectValue /></SelectTrigger>
            <SelectContent>
              {#each typeOptions as option (option.value)}
                <SelectItem value={option.value} label={option.label}>
                  {option.label}
                </SelectItem>
              {/each}
            </SelectContent>
          </Select>
        </div>
      {/if}
      <div class="flex flex-col gap-2">
        <Label for={printerId}>{t('printerCameras.form.printer')}</Label>
        <Select
          value={printer}
          items={printerOptions}
          onValueChange={(next: string) => {
            printer = next;
          }}
        >
          <SelectTrigger id={printerId}><SelectValue /></SelectTrigger>
          <SelectContent>
            {#each printerOptions as option (option.value)}
              <SelectItem value={option.value} label={option.label}>
                {option.label}
              </SelectItem>
            {/each}
          </SelectContent>
        </Select>
      </div>
      {#if takesUrls}
        <Field
          label={t('printerCameras.form.snapshotUrl')}
          {...needsUrl
            ? {}
            : { description: t('printerCameras.form.snapshotUrlNote') }}
          error={urlError}
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
      {/if}
      <DialogFooter>
        <DialogClose>
          {#snippet child({ props })}
            <Button {...props} variant="secondary" disabled={busy}>
              {t('primitives.cancel')}
            </Button>
          {/snippet}
        </DialogClose>
        <Button type="submit" disabled={blocked} aria-busy={busy}>
          {t('printerCameras.form.add')}
        </Button>
      </DialogFooter>
    </form>
  </DialogContent>
</Dialog>
