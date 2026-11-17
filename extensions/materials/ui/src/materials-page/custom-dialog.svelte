<!--
  Create custom material, from the page's header: a material the catalogue
  does not carry belongs in the list whether or not a spool of it exists yet.
  It makes the material, says so, and closes; adding spools of it is the row's
  +.
-->
<script lang="ts">
  import {
    Dialog,
    DialogContent,
    DialogHeader,
    DialogTitle,
    toast,
  } from '@prusa/connect-ui/primitives';

  import CustomMaterialForm from '../add-dialog/custom-material-form.svelte';
  import { type MaterialFields, materialsOn } from '../api';
  import { heldHost } from '../host';
  import { OWN_MODULE } from '../own-module';

  let {
    open,
    onclose,
    oncreated,
  }: {
    open: boolean;
    onclose: () => void;
    oncreated: () => void;
  } = $props();

  const host = heldHost();
  const t = host.t;
  const service = materialsOn(host);

  let busy = $state(false);
  let fault = $state.raw<unknown>(undefined);

  const save = async (fields: MaterialFields): Promise<void> => {
    busy = true;
    fault = undefined;
    try {
      await service.createCustom(fields);
      toast.success(t('materials.custom.created'), {
        description: fields.name,
      });
      oncreated();
      onclose();
    } catch (thrown) {
      fault = thrown;
    } finally {
      busy = false;
    }
  };
</script>

<Dialog
  {open}
  onOpenChange={(next: boolean) => {
    if (!next) onclose();
  }}
>
  <!-- The panel is drawn outside the module's slot, so it carries the
       module's name for its utilities. GuidedDialog caps its panel with this
       very class, and the long form scrolls inside the screen. -->
  <!-- eslint-disable no-restricted-syntax -->
  <DialogContent
    data-module={OWN_MODULE}
    size="lg"
    class="max-h-[calc(100dvh-2rem)] overflow-y-auto"
  >
    <!-- eslint-enable no-restricted-syntax -->
    <DialogHeader>
      <DialogTitle>{t('materials.custom.title')}</DialogTitle>
    </DialogHeader>
    {#if open}
      <CustomMaterialForm
        submitLabel={t('materials.custom.save')}
        {busy}
        {fault}
        onsave={(fields: MaterialFields) => void save(fields)}
        oncancel={onclose}
      />
    {/if}
  </DialogContent>
</Dialog>
