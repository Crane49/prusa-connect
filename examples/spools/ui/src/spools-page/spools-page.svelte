<script lang="ts">
  import {
    Badge,
    Button,
    Card,
    CardContent,
    CardHeader,
    CardTitle,
    Input,
    Label,
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
  } from '@prusa/connect-ui/primitives';
  import { onMount } from 'svelte';

  import { heldHost } from '../host';
  import { heldShelf } from '../shelf.svelte';

  // A page takes no props: the route is the whole context (ADR-20260911-4p6 rule 10).
  // What it draws comes from the shelf the module's setup built, and the shelf
  // reaches the core through the client the host handed over.
  const shelf = heldShelf();
  // Every text this page draws comes from this module's own catalogue,
  // through the `t` the host bound to its namespace (frontend-i18n.md rule 7).
  const t = heldHost().t;

  const MATERIALS = ['PLA', 'PETG', 'ASA', 'PC'].map((value) => ({
    value,
    label: value,
  }));
  const uid = $props.id();

  // Input passes `value` through and does not bind it, so each field writes
  // its own state back.
  type Typed = Event & { currentTarget: HTMLInputElement };

  let id = $state('');
  let material = $state('PLA');
  let colour = $state('');
  let device = $state('');
  let grams = $state(1000);

  const ready = $derived(id.trim() !== '' && colour.trim() !== '');

  // The first read is here and never in an `$effect` (svelte-5.md).
  onMount(() => {
    void shelf.read();
  });

  const submit = async (event: SubmitEvent) => {
    event.preventDefault();
    if (!ready) return;

    const added = await shelf.add({
      id: id.trim(),
      material: material.trim(),
      colour: colour.trim(),
      device: device.trim(),
      grams,
    });

    if (added) {
      id = '';
      colour = '';
      device = '';
    }
  };
</script>

<!-- The page draws its own title row. Merge 41a1efb7 removed the shell tier
     the library used to publish, so `PageLayout` is not there to nest this
     page in, and a module writes the row itself. -->
<section class="flex flex-col gap-4">
  <header class="flex flex-wrap items-center justify-between gap-3">
    <h1 class="text-lg font-semibold">{t('spools.title')}</h1>
    <div data-part="actions" class="flex items-center gap-2">
      <Button
        variant="secondary"
        size="sm"
        disabled={shelf.busy}
        onclick={() => void shelf.read()}
      >
        {t('spools.refresh')}
      </Button>
    </div>
  </header>

  <p class="text-sm text-muted-foreground" data-part="principal">
    {t('spools.principal', { who: shelf.who })}
  </p>

  <p aria-live="polite" class="text-sm text-destructive-text">
    {shelf.problem}
  </p>

  <Card>
    <CardHeader>
      <CardTitle>{t('spools.shelf')}</CardTitle>
    </CardHeader>
    <CardContent>
      {#if !shelf.loaded}
        <p class="text-muted-foreground">{t('spools.reading')}</p>
      {:else if shelf.rows.length === 0}
        <p class="text-muted-foreground">{t('spools.empty')}</p>
      {:else}
        <ul data-part="shelf" class="flex flex-col gap-2">
          {#each shelf.rows as spool (spool.id)}
            <li
              class="flex flex-wrap items-center gap-2 border-b border-border pb-2"
            >
              <span class="font-medium">{spool.id}</span>
              <Badge variant="info">{spool.material}</Badge>
              <span class="text-muted-foreground">{spool.colour}</span>
              <span class="text-muted-foreground">
                {t('spools.weight', { grams: spool.grams })}
              </span>
              {#if spool.device}
                <Badge variant="success">
                  {t('spools.on', { device: spool.device })}
                </Badge>
              {/if}
            </li>
          {/each}
        </ul>
      {/if}
    </CardContent>
  </Card>

  <Card>
    <CardHeader>
      <CardTitle>{t('spools.add')}</CardTitle>
    </CardHeader>
    <CardContent>
      <form class="grid gap-3 @md:grid-cols-2" onsubmit={(e) => void submit(e)}>
        <div class="flex flex-col gap-1">
          <Label for="{uid}-name">{t('spools.name')}</Label>
          <Input
            id="{uid}-name"
            value={id}
            oninput={(event: Typed) => {
              id = event.currentTarget.value;
            }}
            name="id"
            required
          />
        </div>

        <div class="flex flex-col gap-1">
          <Label for="{uid}-material">{t('spools.material')}</Label>
          <Select bind:value={material} name="material" items={MATERIALS}>
            <SelectTrigger id="{uid}-material">
              <SelectValue>{material}</SelectValue>
            </SelectTrigger>
            <SelectContent>
              {#each MATERIALS as one (one.value)}
                <SelectItem value={one.value} label={one.label}
                  >{one.label}</SelectItem
                >
              {/each}
            </SelectContent>
          </Select>
        </div>

        <div class="flex flex-col gap-1">
          <Label for="{uid}-colour">{t('spools.colour')}</Label>
          <Input
            id="{uid}-colour"
            value={colour}
            oninput={(event: Typed) => {
              colour = event.currentTarget.value;
            }}
            name="colour"
            required
          />
        </div>

        <div class="flex flex-col gap-1">
          <Label for="{uid}-device">{t('spools.device')}</Label>
          <Input
            id="{uid}-device"
            value={device}
            oninput={(event: Typed) => {
              device = event.currentTarget.value;
            }}
            name="device"
          />
        </div>

        <div class="flex flex-col gap-1">
          <Label for="{uid}-grams">{t('spools.grams')}</Label>
          <Input
            id="{uid}-grams"
            value={grams}
            oninput={(event: Typed) => {
              grams = event.currentTarget.valueAsNumber;
            }}
            name="grams"
            type="number"
            min="0"
          />
        </div>

        <div class="flex items-end">
          <Button type="submit" disabled={!ready || shelf.busy}>
            {t('spools.submit')}
          </Button>
        </div>
      </form>
    </CardContent>
  </Card>
</section>
