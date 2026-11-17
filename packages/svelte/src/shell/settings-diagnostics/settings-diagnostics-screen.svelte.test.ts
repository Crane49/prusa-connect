import { describe, expect, it, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { DiagnosticsSendError } from '../../data/services/send-diagnostics';
import SettingsDiagnosticsScreen from './settings-diagnostics-screen.svelte';

const consent = () =>
  page
    .getByRole('checkbox', { name: /I agree to share it with the Prusa team/ })
    .click();

describe('SettingsDiagnosticsScreen', () => {
  it('keeps the download off until the person consents', async () => {
    const onDownload = vi.fn().mockResolvedValue(undefined);
    await render(SettingsDiagnosticsScreen, {
      props: { onDownload, onCopy: () => '[]' },
    });
    const download = page.getByRole('button', { name: 'Download diagnostics' });

    await expect.element(download).toBeDisabled();
    await consent();
    await expect.element(download).toBeEnabled();
  });

  it('asks for module output only when the box is ticked', async () => {
    const onDownload = vi.fn().mockResolvedValue(undefined);
    await render(SettingsDiagnosticsScreen, {
      props: { onDownload, onCopy: () => '[]' },
    });
    const download = page.getByRole('button', { name: 'Download diagnostics' });
    await consent();

    await download.click();
    await page
      .getByRole('checkbox', {
        name: 'Include what modules wrote to their error output',
      })
      .click();
    await download.click();

    expect(onDownload.mock.calls).toEqual([
      [{ modules: false, extensions: false }],
      [{ modules: true, extensions: false }],
    ]);
  });

  it('includes the files extensions offer by default, and leaves them out when unticked', async () => {
    const onDownload = vi.fn().mockResolvedValue(undefined);
    await render(SettingsDiagnosticsScreen, {
      props: { onDownload, onCopy: () => '[]', extensions: ['Printer logs'] },
    });
    const download = page.getByRole('button', { name: 'Download diagnostics' });
    const box = page.getByRole('checkbox', {
      name: 'Include files Printer logs keep',
    });
    await consent();

    await expect.element(box).toBeChecked();
    await download.click();
    await box.click();
    await download.click();

    expect(onDownload.mock.calls).toEqual([
      [{ modules: false, extensions: true }],
      [{ modules: false, extensions: false }],
    ]);
  });

  it('draws no extensions choice where no extension offers files', async () => {
    await render(SettingsDiagnosticsScreen, {
      props: { onDownload: vi.fn(), onCopy: () => '[]', extensions: [] },
    });

    await expect
      .element(page.getByRole('checkbox', { name: /files .* keep/ }))
      .not.toBeInTheDocument();
  });

  it('shows a refusal instead of failing silently', async () => {
    const onDownload = vi.fn().mockRejectedValue(new Error('refused'));
    await render(SettingsDiagnosticsScreen, {
      props: { onDownload, onCopy: () => '[]' },
    });

    await consent();
    await page.getByRole('button', { name: 'Download diagnostics' }).click();

    await expect.element(page.getByRole('alert')).toBeVisible();
  });

  it('copies the browser details as they are when the button is pressed', async () => {
    const writeText = vi
      .spyOn(navigator.clipboard, 'writeText')
      .mockResolvedValue(undefined);
    let details = 'before';
    await render(SettingsDiagnosticsScreen, {
      props: {
        onDownload: vi.fn(),
        onCopy: () => details,
      },
    });

    details = 'logged after the screen drew';
    await page.getByRole('button', { name: 'Copy browser details' }).click();

    expect(writeText).toHaveBeenCalledWith('logged after the screen drew');
    writeText.mockRestore();
  });

  it('offers no sending where the server sends nowhere', async () => {
    await render(SettingsDiagnosticsScreen, {
      props: { onDownload: vi.fn(), onCopy: () => '[]' },
    });

    expect(page.getByRole('button', { name: 'Send to Prusa' }).query()).toBe(
      null,
    );
    expect(page.getByRole('radio').query()).toBe(null);
  });

  it('sends only after consent, with the modules box and the chosen intent', async () => {
    const onSend = vi.fn().mockResolvedValue(undefined);
    await render(SettingsDiagnosticsScreen, {
      props: { onDownload: vi.fn(), onCopy: () => '[]', onSend },
    });
    const send = page.getByRole('button', { name: 'Send to Prusa' });

    await expect.element(send).toBeDisabled();
    await expect
      .element(
        page.getByText('The file goes to the Prusa beta portal.', {
          exact: false,
        }),
      )
      .toBeVisible();
    await consent();
    await send.click();
    await page
      .getByRole('radio', { name: 'Add to a report I already sent' })
      .click();
    await page
      .getByRole('checkbox', {
        name: 'Include what modules wrote to their error output',
      })
      .click();
    await send.click();

    expect(onSend.mock.calls).toEqual([
      [{ modules: false, extensions: false }, 'new'],
      [{ modules: true, extensions: false }, 'existing'],
    ]);
  });

  it('shows a refusal from the portal by what it means', async () => {
    const onSend = vi
      .fn()
      .mockRejectedValue(
        new DiagnosticsSendError(413, 'too_large', 'File exceeds 25 MB'),
      );
    await render(SettingsDiagnosticsScreen, {
      props: { onDownload: vi.fn(), onCopy: () => '[]', onSend },
    });

    await consent();
    await page.getByRole('button', { name: 'Send to Prusa' }).click();

    const alert = page.getByRole('alert');
    await expect.element(alert).toBeVisible();
    const text = alert.element().textContent;

    expect(text).toContain('The file is too large for the Prusa beta portal.');
    expect(text).toContain('File exceeds 25 MB');
    expect(text).toContain('too_large');
  });
});
