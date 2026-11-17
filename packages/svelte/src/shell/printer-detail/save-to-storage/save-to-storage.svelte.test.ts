import { describe, expect, it, vi } from 'vitest';
import { page, userEvent } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import SaveToStorage from './save-to-storage.svelte';
import type { SaveToStorageProps } from './types';

const props = (over: Partial<SaveToStorageProps> = {}): SaveToStorageProps => ({
  keep: false,
  onKeepChange: vi.fn(),
  storages: [{ name: 'local', label: 'Local' }],
  storage: 'local',
  onStorageChange: vi.fn(),
  folder: '',
  onFolderChange: vi.fn(),
  pickerAt: '',
  pickerFolders: [],
  onPickerWalk: vi.fn(),
  ...over,
});

describe('SaveToStorage', () => {
  it('starts off, says the file is deleted after the send, and offers no folder', async () => {
    await render(SaveToStorage, props());

    const toggle = page.getByRole('switch', { name: 'Save to storage' });
    await expect.element(toggle).toHaveAttribute('aria-checked', 'false');
    await expect
      .element(
        page.getByText(
          'The file goes to the printer and is then deleted from this server.',
        ),
      )
      .toBeVisible();
    expect(
      page.getByRole('button', { name: 'Choose folder' }).elements(),
    ).toHaveLength(0);
  });

  it('asks the host to turn it on by click and by Space', async () => {
    const onKeepChange = vi.fn();
    await render(SaveToStorage, props({ onKeepChange }));

    const toggle = page.getByRole('switch', { name: 'Save to storage' });
    await toggle.click();
    expect(onKeepChange).toHaveBeenLastCalledWith(true);

    onKeepChange.mockClear();
    (toggle.element() as HTMLElement).focus();
    await userEvent.keyboard(' ');
    expect(onKeepChange).toHaveBeenCalledTimes(1);
  });

  it('on, names the storage and the folder as one trail, and offers no storage choice for one storage', async () => {
    await render(
      SaveToStorage,
      props({ keep: true, folder: 'prints/brackets' }),
    );

    await expect
      .element(page.getByText('Local / prints / brackets'))
      .toBeVisible();
    await expect
      .element(
        page.getByText(
          'The file stays in this folder after it reaches the printer.',
        ),
      )
      .toBeVisible();
    expect(
      page.getByRole('combobox', { name: 'Storage' }).elements(),
    ).toHaveLength(0);
  });

  it('offers the storages to choose between when there is more than one', async () => {
    await render(
      SaveToStorage,
      props({
        keep: true,
        storages: [
          { name: 'local', label: 'Local' },
          { name: 'nas', label: 'Workshop NAS' },
        ],
        storage: 'nas',
      }),
    );

    await expect
      .element(page.getByRole('combobox', { name: 'Storage' }))
      .toHaveTextContent('Workshop NAS');
  });

  it('opens the picker on the chosen folder, walks it, and hands back the folder taken', async () => {
    const onPickerWalk = vi.fn();
    const onFolderChange = vi.fn();
    await render(
      SaveToStorage,
      props({
        keep: true,
        folder: 'prints',
        pickerAt: 'prints',
        pickerFolders: [{ name: 'brackets', path: 'prints/brackets' }],
        onPickerWalk,
        onFolderChange,
      }),
    );

    await page.getByRole('button', { name: 'Choose folder' }).click();
    expect(onPickerWalk).toHaveBeenLastCalledWith('prints');

    await expect
      .element(page.getByRole('dialog', { name: 'Save the file in' }))
      .toBeVisible();

    await page.getByRole('button', { name: 'brackets' }).click();
    expect(onPickerWalk).toHaveBeenLastCalledWith('prints/brackets');

    await page.getByRole('button', { name: 'Save here' }).click();
    expect(onFolderChange).toHaveBeenCalledWith('prints');
    await expect
      .element(page.getByRole('dialog', { name: 'Save the file in' }))
      .not.toBeInTheDocument();
  });

  it('says why the folders could not be read', async () => {
    await render(
      SaveToStorage,
      props({
        keep: true,
        pickerFault: {
          code: 'core.storage/UNAVAILABLE',
          message: 'the storage could not carry that out',
        },
      }),
    );

    await page.getByRole('button', { name: 'Choose folder' }).click();
    await expect.element(page.getByRole('alert')).toBeVisible();
  });
});
