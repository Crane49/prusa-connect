// The largest upload of each staged route an install approved: shown at the
// limit it holds, saved as typed, and refused outside 1 to 4096 MiB.
import { describe, expect, it, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import ExtensionUploads from './extension-uploads.svelte';

const upload = {
  method: 'PUT',
  path: '/connect/app/teams/{team}/files/raw',
  max_mib: 1024,
};

const field = () =>
  page.getByRole('spinbutton', {
    name: 'Largest upload on PUT /connect/app/teams/{team}/files/raw',
  });
const save = () => page.getByRole('button', { name: 'Save the limits' });

describe('ExtensionUploads', () => {
  it('shows the approved limit and saves a changed one', async () => {
    const onsave = vi.fn().mockResolvedValue(true);
    await render(ExtensionUploads, {
      props: { routes: [upload], asked: [upload], saving: false, onsave },
    });

    await expect.element(field()).toHaveValue(1024);
    await expect
      .element(page.getByText('The extension asks for 1,024 MiB.'))
      .toBeVisible();
    await expect.element(save()).toBeDisabled();

    await field().fill('3000');
    await save().click();

    expect(onsave).toHaveBeenCalledWith([{ ...upload, max_mib: 3000 }]);
  });

  it('refuses a limit outside 1 to 4096 MiB', async () => {
    const onsave = vi.fn().mockResolvedValue(true);
    await render(ExtensionUploads, {
      props: { routes: [upload], saving: false, onsave },
    });

    for (const typed of ['0', '4097', '1.5']) {
      await field().fill(typed);
      await expect
        .element(page.getByText('A whole number of MiB from 1 to 4,096'))
        .toBeVisible();
      await expect.element(field()).toHaveAttribute('aria-invalid', 'true');
      await expect.element(save()).toBeDisabled();
    }

    await field().fill('4096');
    await expect.element(save()).toBeEnabled();
  });

  it('blocks the field and the save while a save is running', async () => {
    const onsave = vi.fn();
    await render(ExtensionUploads, {
      props: { routes: [upload], saving: true, onsave },
    });

    await expect.element(field()).toBeDisabled();
    await expect.element(save()).toHaveAttribute('aria-busy', 'true');
    await expect.element(save()).toBeDisabled();
  });

  it('keeps what was typed when a save fails, and says why', async () => {
    const onsave = vi.fn().mockResolvedValue(false);
    await render(ExtensionUploads, {
      props: {
        routes: [upload],
        saving: false,
        fault: new Error('the server went away'),
        onsave,
      },
    });

    await expect.element(page.getByRole('alert')).toBeVisible();

    await field().fill('3000');
    await save().click();

    expect(onsave).toHaveBeenCalledOnce();
    await expect.element(field()).toHaveValue(3000);
    await expect.element(save()).toBeEnabled();
  });

  it('says the extension changed meanwhile when the save met another', async () => {
    await render(ExtensionUploads, {
      props: {
        routes: [upload],
        saving: false,
        fault: new Error('not shown'),
        conflict: true,
        onsave: vi.fn(),
      },
    });

    await expect
      .element(
        page.getByText(
          'This extension changed meanwhile. What you see now is current.',
        ),
      )
      .toBeVisible();
    // The warning is the one alert: the fault of that save is not shown too.
    expect(page.getByRole('alert').elements()).toHaveLength(1);
  });

  it('says why the limits cannot be changed, and offers no save', async () => {
    await render(ExtensionUploads, {
      props: {
        routes: [upload],
        locked: 'Locked for the test.',
        saving: false,
        onsave: vi.fn(),
      },
    });

    await expect.element(page.getByText('Locked for the test.')).toBeVisible();
    await expect.element(field()).toBeDisabled();
    await expect.element(save()).not.toBeInTheDocument();
  });
});
