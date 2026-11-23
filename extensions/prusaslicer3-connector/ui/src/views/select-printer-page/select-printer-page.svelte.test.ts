import { describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { farm, mk4sRequest, pickerOf, t } from '../../test/fakes';
import SelectPrinterPage from './select-printer-page.svelte';

describe('the print dialog page', () => {
  it('offers the printers that take the file, with the ones that cannot print disabled', async () => {
    await render(SelectPrinterPage, { props: { view: pickerOf(), t } });

    await expect.element(page.getByRole('radio')).toHaveLength(4);
    await expect
      .element(page.getByRole('radio', { name: /Workshop MK4S/ }))
      .toBeChecked();
    await expect
      .element(page.getByRole('radio', { name: /Office MK4S/ }))
      .toBeDisabled();
    await expect
      .element(page.getByRole('radio', { name: /Lab MK4S/ }))
      .toBeEnabled();
    await expect.element(page.getByText('Big XL')).not.toBeInTheDocument();
    // The Lab printer holds PETG and a 0.6 nozzle.
    await expect
      .element(page.getByText('The nozzle diameter is different'))
      .toBeVisible();
    await expect
      .element(page.getByLabelText('File name'))
      .toHaveValue(mk4sRequest.filename ?? '');
  });

  it('prints and cancels through the store', async () => {
    const view = pickerOf();
    await render(SelectPrinterPage, { props: { view, t } });

    await page.getByRole('button', { name: 'Print' }).click();
    await page.getByRole('button', { name: 'Cancel' }).click();

    expect(view.posted).toEqual(['PRINT', 'CLOSE_DIALOG']);
  });

  it('blocks Print without a file name and says why', async () => {
    const view = pickerOf({ filename: '' });
    await render(SelectPrinterPage, { props: { view, t } });

    await expect
      .element(page.getByRole('button', { name: 'Print' }))
      .toBeDisabled();
    await expect
      .element(page.getByTestId('print-hint'))
      .toHaveTextContent('Enter a file name');
  });

  it('replaces the file name with a note for a project of several beds', async () => {
    await render(SelectPrinterPage, {
      props: {
        view: pickerOf({ request: { ...mk4sRequest, multipleBeds: true } }),
        t,
      },
    });

    await expect
      .element(
        page
          .getByTestId('multiple-beds')
          .getByText(/Only the first bed prints/),
      )
      .toBeVisible();
    await expect
      .element(page.getByLabelText('File name'))
      .not.toBeInTheDocument();
  });

  it('says no printer takes the file', async () => {
    await render(SelectPrinterPage, {
      props: {
        view: pickerOf({ request: { printerModel: 'MINI' }, printers: farm }),
        t,
      },
    });

    await expect
      .element(page.getByRole('heading', { name: 'No compatible printers' }))
      .toBeVisible();
  });

  it('says the person may use no printer at all', async () => {
    await render(SelectPrinterPage, {
      props: { view: pickerOf({ printers: [] }), t },
    });

    await expect
      .element(page.getByRole('heading', { name: 'No printers' }))
      .toBeVisible();
  });

  it('waits for the slicer, and is busy while it does', async () => {
    await render(SelectPrinterPage, {
      props: { view: pickerOf({ request: undefined }), t },
    });

    await expect
      .element(page.getByRole('status'))
      .toHaveAttribute('aria-busy', 'true');
    await expect
      .element(page.getByRole('button', { name: 'Print' }))
      .toBeDisabled();
  });

  it('offers a retry when the printers could not be read', async () => {
    const view = pickerOf({ load: 'failed' });
    await render(SelectPrinterPage, { props: { view, t } });

    await page.getByRole('button', { name: 'Try again' }).click();

    expect(view.posted).toEqual(['refresh']);
  });

  it('offers no retry for a refusal', async () => {
    await render(SelectPrinterPage, {
      props: { view: pickerOf({ load: 'refused' }), t },
    });

    await expect.element(page.getByRole('alert')).toBeVisible();
    await expect
      .element(page.getByRole('button', { name: 'Try again' }))
      .not.toBeInTheDocument();
  });

  it('says it is for PrusaSlicer outside it, with no buttons', async () => {
    await render(SelectPrinterPage, {
      props: { view: pickerOf({ inSlicer: false }), t },
    });

    await expect
      .element(
        page.getByRole('heading', { name: 'This page is for PrusaSlicer' }),
      )
      .toBeVisible();
    await expect
      .element(page.getByRole('button', { name: 'Print' }))
      .not.toBeInTheDocument();
  });
});
