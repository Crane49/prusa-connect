<!--
  Asks a printer to send its log lines and metrics here. Each stream is
  switched on or off alone, which the firmware does by port 0. After the
  request the person is told to confirm on the printer's display, because
  nothing changes until they do.

  A person who may not send G-code is told so by the code, and shown the
  steps on the printer instead.
-->
<script lang="ts">
  import {
    Alert,
    AlertDescription,
    AlertTitle,
    Button,
    Dialog,
    DIALOG_BODY,
    DIALOG_SCROLLS,
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
    Switch,
  } from '@prusa/connect-ui/primitives';

  import { faultText } from '../faults';
  import { MAX_HOST } from '../presentation';
  import { heldSession } from '../session';
  import { portsOf, type SetupStore } from '../setup.svelte';
  import SetupGuide from './setup-guide.svelte';

  // The caller opens it with `SetupStore.open`, which reads the
  // address in the click and never in an effect.
  let {
    setup,
    open,
    deviceId,
    onClose,
  }: {
    setup: SetupStore;
    open: boolean;
    /** The printer, when the dialog was opened from its page. */
    deviceId?: string | undefined;
    onClose: () => void;
  } = $props();

  const { host, status } = heldSession();
  const t = host.t;
  const uid = $props.id();

  let chosen = $state('');
  // What the person typed, over the address the app names for printers.
  let typed = $state<string | undefined>(undefined);
  const address = $derived(typed ?? setup.host ?? '');
  let syslog = $state<boolean>(true);
  let metrics = $state<boolean>(true);
  // Whether the last request asked the printer to stop sending, which the
  // steps after it say in their own words.
  let stopping = $state(false);

  const ports = $derived(portsOf(status.status));
  const printers = $derived(
    host.fleet.devices.map((one) => ({
      value: one.id,
      label: one.name || one.id,
    })),
  );
  const target = $derived(deviceId ?? chosen);
  const ready = $derived(
    target !== '' &&
      address.trim().length >= 2 &&
      address.trim().length <= MAX_HOST,
  );
  const fault = $derived(
    setup.fault === undefined ? undefined : faultText(setup.fault, t),
  );

  const submit = (event: SubmitEvent): void => {
    event.preventDefault();
    if (!ready) return;
    stopping = !syslog && !metrics;
    void setup.send(
      { deviceId: target, host: address, syslog, metrics },
      ports,
    );
  };

  // Both streams off is a bare M334, which switches Metrics & Log off on the
  // printer and keeps its host and ports.
  const stop = (): void => {
    if (!ready) return;
    stopping = true;
    void setup.send(
      { deviceId: target, host: address, syslog: false, metrics: false },
      ports,
    );
  };
</script>

<Dialog
  {open}
  onOpenChange={(next: boolean) => {
    if (!next) onClose();
  }}
>
  <DialogContent class={DIALOG_SCROLLS} data-part="setup-dialog">
    <DialogHeader>
      <DialogTitle>{t('buddyDebugLogs.setup.title')}</DialogTitle>
      <DialogDescription>{t('buddyDebugLogs.setup.note')}</DialogDescription>
    </DialogHeader>

    {#if setup.sent}
      <div class={['grid gap-3', DIALOG_BODY]}>
        <Alert variant="warning" data-part="setup-sent" aria-live="polite">
          <AlertTitle>{t('buddyDebugLogs.setup.waitTitle')}</AlertTitle>
          <AlertDescription>
            <ol class="list-decimal ps-5">
              <li>{t('buddyDebugLogs.setup.waitStep1')}</li>
              <li>{t('buddyDebugLogs.setup.waitStep2')}</li>
              <li>
                {stopping
                  ? t('buddyDebugLogs.setup.waitStep3Stop')
                  : t('buddyDebugLogs.setup.waitStep3')}
              </li>
            </ol>
          </AlertDescription>
        </Alert>
        <p class="text-sm text-muted-foreground">
          {t('buddyDebugLogs.setup.waitNo')}
        </p>
        <code class="text-xs text-muted-foreground">{setup.sent.gcode}</code>
      </div>
      <DialogFooter>
        <Button onclick={onClose}>{t('buddyDebugLogs.setup.close')}</Button>
      </DialogFooter>
    {:else}
      <!-- `contents`, so the body and the footer are the dialog's own rows
           and only the body scrolls. -->
      <form class="contents" onsubmit={submit}>
        <div class={['grid gap-y-4 pe-1', DIALOG_BODY]}>
          <FaultAlert {fault} />
          <Alert variant="info" data-part="setup-confirm-note">
            <AlertTitle>{t('buddyDebugLogs.setup.confirmTitle')}</AlertTitle>
            <AlertDescription
              >{t('buddyDebugLogs.setup.confirmNote')}</AlertDescription
            >
          </Alert>
          {#if setup.notAllowed}
            <p class="text-sm" data-part="setup-fallback">
              {t('buddyDebugLogs.setup.manualFallback')}
            </p>
            <SetupGuide
              host={address}
              syslogPort={ports.syslog}
              metricsPort={ports.metrics}
              open
            />
          {/if}

          {#if deviceId === undefined}
            <div class="grid gap-1.5">
              <Label for={`${uid}-printer`}
                >{t('buddyDebugLogs.setup.printer')}</Label
              >
              <Select
                value={chosen}
                items={printers}
                onValueChange={(next: string) => {
                  chosen = next;
                }}
              >
                <SelectTrigger id={`${uid}-printer`} class="w-full">
                  <SelectValue
                    placeholder={t('buddyDebugLogs.setup.choosePrinter')}
                  />
                </SelectTrigger>
                <SelectContent>
                  {#each printers as printer (printer.value)}
                    <SelectItem value={printer.value} label={printer.label}>
                      {printer.label}
                    </SelectItem>
                  {/each}
                </SelectContent>
              </Select>
            </div>
          {/if}

          <Field
            label={t('buddyDebugLogs.setup.host')}
            description={t('buddyDebugLogs.setup.hostHelp')}
            control={Input}
            controlProps={{
              name: 'host',
              required: true,
              maxlength: MAX_HOST,
              autocomplete: 'off',
              value: address,
              oninput: (event: Event) => {
                typed = (event.currentTarget as HTMLInputElement).value;
              },
            }}
          />

          <div class="flex items-start justify-between gap-3">
            <div class="grid gap-0.5">
              <Label for={`${uid}-syslog`}
                >{t('buddyDebugLogs.setup.syslog')}</Label
              >
              <span class="text-xs text-muted-foreground">
                {t('buddyDebugLogs.setup.syslogHelp', {
                  port: String(ports.syslog),
                })}
              </span>
            </div>
            <Switch id={`${uid}-syslog`} bind:checked={syslog} />
          </div>
          <div class="flex items-start justify-between gap-3">
            <div class="grid gap-0.5">
              <Label for={`${uid}-metrics`}
                >{t('buddyDebugLogs.setup.metrics')}</Label
              >
              <span class="text-xs text-muted-foreground">
                {t('buddyDebugLogs.setup.metricsHelp', {
                  port: String(ports.metrics),
                })}
              </span>
            </div>
            <Switch id={`${uid}-metrics`} bind:checked={metrics} />
          </div>
          {#if !syslog && !metrics}
            <p class="text-xs text-muted-foreground">
              {t('buddyDebugLogs.setup.bothOff')}
            </p>
          {/if}
        </div>

        <DialogFooter>
          <Button
            variant="destructive"
            type="button"
            class="sm:me-auto"
            disabled={!ready || setup.sending}
            onclick={stop}
            data-part="setup-stop"
          >
            {t('buddyDebugLogs.setup.stop')}
          </Button>
          <Button variant="secondary" type="button" onclick={onClose}>
            {t('buddyDebugLogs.setup.cancel')}
          </Button>
          <Button
            type="submit"
            disabled={!ready || setup.sending}
            aria-busy={setup.sending}
          >
            {t('buddyDebugLogs.setup.send')}
          </Button>
        </DialogFooter>
      </form>
    {/if}
  </DialogContent>
</Dialog>
