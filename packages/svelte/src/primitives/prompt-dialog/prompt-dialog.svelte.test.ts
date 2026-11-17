import { describe, expect, it } from 'vitest';
import { page, userEvent } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { expectNoViolations } from '../../test/axe';
import PromptDialogNewFolder from '../../test/prompt-dialog-new-folder.svelte';
import PromptDialogRename from '../../test/prompt-dialog-rename.svelte';

const dialog = () => page.getByRole('dialog');
const open = async (name: string): Promise<void> => {
  await page.getByRole('button', { name }).click();
  await expect.element(dialog()).toBeInTheDocument();
};

describe('PromptDialog', () => {
  it('opens with the field focused, and refuses an empty name', async () => {
    await render(PromptDialogNewFolder);

    await open('New folder');

    const field = page.getByRole('textbox', { name: 'Folder name' });
    await expect.element(field).toHaveFocus();
    await expect
      .element(page.getByRole('button', { name: 'Create' }))
      .toBeDisabled();
    await expectNoViolations(dialog().element());
  });

  it('blocks confirm and says why when the name is taken', async () => {
    await render(PromptDialogNewFolder);
    await open('New folder');

    await page.getByRole('textbox', { name: 'Folder name' }).fill('archive');

    await expect
      .element(
        page.getByText('A file or folder with that name already exists.'),
      )
      .toBeVisible();
    await expect
      .element(page.getByRole('button', { name: 'Create' }))
      .toBeDisabled();
  });

  // Enter confirms the way pressing Create does. A dialog with one field has
  // no other control to reach for.
  it('creates on Enter, shuts the panel and reports what was made', async () => {
    await render(PromptDialogNewFolder);
    await open('New folder');

    await page.getByRole('textbox', { name: 'Folder name' }).fill('parts');
    await userEvent.keyboard('{Enter}');

    await expect.element(dialog()).not.toBeInTheDocument();
    await expect.element(page.getByText('parts created.')).toBeVisible();
  });

  it('sends nothing when Enter is pressed on a name that is blocked', async () => {
    await render(PromptDialogNewFolder);
    await open('New folder');

    await page.getByRole('textbox', { name: 'Folder name' }).fill('archive');
    await userEvent.keyboard('{Enter}');

    await expect.element(dialog()).toBeInTheDocument();
  });

  it('throws the draft away when the panel is cancelled', async () => {
    await render(PromptDialogNewFolder);
    await open('New folder');

    await page.getByRole('textbox', { name: 'Folder name' }).fill('abandoned');
    await page.getByRole('button', { name: 'Cancel' }).click();
    await expect.element(dialog()).not.toBeInTheDocument();

    await open('New folder');
    await expect
      .element(page.getByRole('textbox', { name: 'Folder name' }))
      .toHaveValue('');
  });

  it('puts the current name back when a rename is cancelled', async () => {
    await render(PromptDialogRename, { props: { currentName: 'benchy.3mf' } });
    await open('Rename');

    const field = page.getByRole('textbox', { name: 'Name' });
    await expect.element(field).toHaveValue('benchy.3mf');
    await field.fill('renamed.gcode');
    await page.getByRole('button', { name: 'Cancel' }).click();
    await expect.element(dialog()).not.toBeInTheDocument();

    await open('Rename');
    await expect
      .element(page.getByRole('textbox', { name: 'Name' }))
      .toHaveValue('benchy.3mf');
  });

  it('shuts on Escape, and reports it so the draft goes with it', async () => {
    await render(PromptDialogNewFolder);
    await open('New folder');

    await page.getByRole('textbox', { name: 'Folder name' }).fill('abandoned');
    await userEvent.keyboard('{Escape}');
    await expect.element(dialog()).not.toBeInTheDocument();

    await open('New folder');
    await expect
      .element(page.getByRole('textbox', { name: 'Folder name' }))
      .toHaveValue('');
  });
});
