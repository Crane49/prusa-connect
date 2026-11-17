import { describe, expect, it } from 'vitest';
import { page, userEvent } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { expectNoViolations } from '../test/axe';
import ProjectPartProbe from '../test/project-part-probe.svelte';
import {
  generic,
  manyBeds,
  multibed,
  parked,
  twoSetups,
} from '../test/project-fixtures';
import { mmuObjects } from '../test/project-fixtures-multimaterial';
import type { ProjectBrowser } from './browser.svelte';
import type { Project } from './model';

const copies = (): HTMLElement[] => [
  ...document.querySelectorAll<HTMLElement>('[data-copy]'),
];

const plan = (project: Project, view?: string) => {
  let browser: ProjectBrowser | undefined;
  const rendered = render(ProjectPartProbe, {
    props: {
      project,
      parts: ['plan'],
      view,
      onbrowser: (made: ProjectBrowser) => {
        browser = made;
      },
    },
  });

  return {
    rendered,
    browser: (): ProjectBrowser => {
      if (browser === undefined) throw new Error('no browser');
      return browser;
    },
  };
};

describe('ProjectPlan', () => {
  it('draws every bed and every copy, and names each copy', async () => {
    await plan(multibed, 'all').rendered;

    await expect
      .element(page.getByRole('group', { name: /Plan of the beds/ }))
      .toBeVisible();
    expect(document.querySelectorAll('.bed')).toHaveLength(3);
    expect(copies()).toHaveLength(multibed.counts.instances);
    await expect
      .element(page.getByRole('button', { name: /^CubeA, copy 1 of 4/ }))
      .toBeInTheDocument();
    await expectNoViolations(document.body);
  });

  it('draws one bed alone, and only its copies', async () => {
    await plan(multibed, '0:1').rendered;

    await expect
      .element(page.getByRole('group', { name: 'Plan of Bed 2, to scale' }))
      .toBeVisible();
    expect(document.querySelectorAll('.bed')).toHaveLength(1);
    expect(copies()).toHaveLength(
      multibed.setups[0]?.beds[1]?.instances.length ?? -1,
    );
  });

  it('gives the copies one tab stop and moves between them by arrow', async () => {
    await plan(multibed, 'all').rendered;

    const stops = copies().filter((copy) => copy.tabIndex === 0);
    expect(stops).toHaveLength(1);

    const [first, second] = copies();
    first?.focus();
    await userEvent.keyboard('{ArrowRight}');
    expect(document.activeElement).toBe(second);
    await userEvent.keyboard('{ArrowLeft}');
    expect(document.activeElement).toBe(first);
    await userEvent.keyboard('{End}');
    expect(document.activeElement).toBe(copies().at(-1));
  });

  it('ticks a copy with Enter, marks its object, and lets go with Escape', async () => {
    const { rendered, browser } = plan(multibed, 'all');
    await rendered;

    copies()[0]?.focus();
    await userEvent.keyboard('{Enter}');
    expect(browser().isSelected(0, 0)).toBe(true);
    expect(browser().active).toBe(0);
    expect(copies()[0]?.getAttribute('aria-pressed')).toBe('true');

    await userEvent.keyboard('{Escape}');
    expect(browser().active).toBeUndefined();
    expect(browser().isSelected(0, 0)).toBe(true);
  });

  it('fades the copies that are not ticked once one is', async () => {
    const { rendered, browser } = plan(multibed, 'all');
    await rendered;

    await userEvent.click(
      page.getByRole('button', { name: /^CubeB, copy 1 of 2/ }),
    );

    expect(browser().selection.count).toBe(1);
    const svg = document.querySelector('[data-slot="project-plan"] svg');
    expect(svg?.getAttribute('data-ticking')).toBe('true');
    // The fade is a transition, so the test waits for where it ends.
    await expect
      .poll(
        () =>
          copies().filter(
            (copy) =>
              copy.dataset.selected === 'false' &&
              getComputedStyle(copy.querySelector('polygon') as Element)
                .opacity === '0.35',
          ).length,
      )
      .toBe(multibed.counts.instances - 1);
  });

  it('opens the object of a copy pressed twice', async () => {
    const { rendered, browser } = plan(multibed, 'all');
    await rendered;

    await userEvent.dblClick(
      page.getByRole('button', { name: /^CubeB, copy 1 of 2/ }),
    );

    expect(browser().opened).toBe(1);
  });

  it('draws each copy in the colour of its filament', async () => {
    await plan(mmuObjects).rendered;

    const styles = copies().map((copy) => copy.getAttribute('style') ?? '');
    expect(styles.every((style) => style.includes('--copy: #'))).toBe(true);
  });

  it('shows what a copy is when the pointer is on it', async () => {
    const { rendered, browser } = plan(multibed, 'all');
    await rendered;

    await userEvent.hover(page.getByRole('button', { name: /^CubeB, copy 1/ }));
    await expect.element(page.getByRole('status')).toBeVisible();
    const tip = page
      .getByRole('status')
      .element()
      .textContent.replace(/\s+/g, ' ')
      .trim();
    expect(tip).toContain('CubeB, copy 1 of 2');
    expect(tip).toContain('Printable, On Bed 1');
    expect(browser().hovered).toBe(1);
  });

  it('opens a bed alone when the bed itself is pressed', async () => {
    await plan(twoSetups, 'all').rendered;

    await userEvent.click(
      page.getByRole('button', { name: 'Show MINI, bed 1 alone' }),
    );

    await expect
      .element(
        page.getByRole('group', { name: 'Plan of MINI, bed 1, to scale' }),
      )
      .toBeVisible();
  });

  it('says a bed of unknown size is drawn around its copies', async () => {
    await plan(generic).rendered;

    await expect
      .element(page.getByText(/does not give the size of this bed/))
      .toBeVisible();
    expect(document.querySelectorAll('.bed-unknown')).toHaveLength(1);
  });

  it('names in the legend only what colour does not say', async () => {
    await plan(manyBeds, 'all').rendered;

    const legend = page.getByRole('list', { name: 'Colours' });
    await expect.element(legend).toBeVisible();
    const text = legend.element().textContent;
    expect(text).toContain('Not printable');
    expect(text).toContain('Partly outside its bed');
    expect(text).toContain('On no bed');
    expect(text).not.toContain('Ticked');
  });

  it('draws a parked copy off the beds, dashed and outlined', async () => {
    await plan(parked, 'all').rendered;

    await expect.element(page.getByText('Not on any bed')).toBeVisible();
    await expect
      .element(page.getByText('Empty', { exact: true }))
      .toBeVisible();
    const sign = document.querySelector<SVGGElement>('[data-copy="5:0"]');
    expect(sign?.dataset.printable).toBe('false');
    expect(sign?.dataset.placement).toBe('outside');
    expect(sign?.getAttribute('aria-label')).toMatch(
      /Not printable, On no bed/,
    );
    // A copy that cannot be ticked is no toggle.
    expect(sign?.hasAttribute('aria-pressed')).toBe(false);
    const halfOff = document.querySelector<SVGGElement>('[data-copy="2:0"]');
    expect(halfOff?.dataset.placement).toBe('partlyOutside');
  });

  it('draws only the copies on no bed in their own view', async () => {
    await plan(parked, 'off').rendered;

    await expect
      .element(page.getByRole('group', { name: /copies on no bed/ }))
      .toBeVisible();
    expect(document.querySelectorAll('.bed')).toHaveLength(0);
    expect(copies().map((copy) => copy.dataset.copy)).toEqual(['5:0']);
  });

  it('draws no label wider than its copy', async () => {
    await plan(parked, 'all').rendered;

    const svg = document.querySelector('[data-slot="project-plan"] svg');
    const labels = [...(svg?.querySelectorAll('g[aria-hidden] text') ?? [])];
    for (const label of labels) {
      const box = label.getBoundingClientRect();
      const copy = [...document.querySelectorAll('[data-copy] polygon')].find(
        (shape) => {
          const at = shape.getBoundingClientRect();
          return (
            at.left <= box.left + box.width / 2 &&
            at.right >= box.left + box.width / 2 &&
            at.top <= box.top + box.height / 2 &&
            at.bottom >= box.top + box.height / 2
          );
        },
      );
      const around = copy?.getBoundingClientRect();
      expect(around).toBeDefined();
      expect(box.left).toBeGreaterThanOrEqual((around?.left ?? 0) - 1);
      expect(box.right).toBeLessThanOrEqual((around?.right ?? 0) + 1);
    }
  });
});
