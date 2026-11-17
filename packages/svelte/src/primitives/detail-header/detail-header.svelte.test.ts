import { describe, expect, it } from 'vitest';
import { render } from 'vitest-browser-svelte';

import { expectNoViolations } from '../../test/axe';
import DetailHeaderProbe from '../../test/detail-header-probe.svelte';

describe('DetailHeader', () => {
  it('draws the title as the page heading', async () => {
    await render(DetailHeaderProbe, { props: { title: 'MK4S' } });

    const heading = document.querySelector('h1');
    expect(heading?.textContent.trim()).toBe('MK4S');
  });

  // A name the page is about is a name every character of which belongs on
  // screen, and two faces in one component is two design languages.
  it('wraps the title and never truncates or switches face', async () => {
    await render(DetailHeaderProbe, {
      props: {
        title: 'a-very-long-printer-name-that-will-not-fit-on-one-line',
      },
    });

    const heading = document.querySelector('h1');
    const className = heading?.className ?? '';
    expect(className).not.toContain('truncate');
    expect(className).not.toContain('font-mono');
    expect(className).not.toContain('text-ellipsis');
    expect(getComputedStyle(heading as Element).textOverflow).toBe('clip');
  });

  it('draws the four parts the caller fills', async () => {
    const { container } = await render(DetailHeaderProbe, {
      props: { title: 'MK4S', parts: true },
    });

    expect(container.querySelector('[data-testid="media"]')).not.toBeNull();
    expect(
      container.querySelector('[data-testid="title-aside"]'),
    ).not.toBeNull();
    expect(container.querySelector('[data-testid="meta"]')).not.toBeNull();
    expect(container.querySelector('[data-testid="actions"]')).not.toBeNull();
  });

  // The state is the one fact a reader opens a detail screen for, and pinned
  // under the name it is a second glance away from what it describes.
  it("keeps the aside on the title's own line", async () => {
    const { container } = await render(DetailHeaderProbe, {
      props: { title: 'MK4S', parts: true },
    });

    const heading = container.querySelector('h1');
    const aside = container.querySelector('[data-testid="title-aside"]');
    expect(heading?.parentElement).toBe(aside?.parentElement);
    expect(heading?.parentElement?.className).not.toContain('flex-wrap');
  });

  // A slicer's file name is one unbroken word long enough to push everything
  // beside it off the line.
  it('clips a long name in the middle and keeps its end', async () => {
    const name =
      'Bracket_Left_Reinforced_v4_FINAL_0.4n_0.2mm_PLA_MK4IS_5h12m.bgcode';
    const { container } = await render(DetailHeaderProbe, {
      props: { title: name, parts: true },
    });

    const heading = container.querySelector('h1');
    expect(heading?.textContent).toBe(name);
    // The end is what tells two of them apart, so the end never shrinks.
    expect(
      heading?.querySelector('[data-slot="clipped-name"]')?.lastElementChild
        ?.textContent,
    ).toBe('5h12m.bgcode');
  });

  // The header's own shape, not a grey page: a picture, a title and two lines.
  it('draws its own skeleton while it loads, and nothing of the subject', async () => {
    const { container } = await render(DetailHeaderProbe, {
      props: { title: 'MK4S', parts: true, loading: true },
    });

    expect(container.querySelectorAll('[data-slot="skeleton"]')).toHaveLength(
      4,
    );
    expect(document.querySelector('h1')).toBeNull();
    expect(container.querySelector('[data-testid="actions"]')).toBeNull();
  });

  it('names its own container so a caller sets no geometry', async () => {
    const { container } = await render(DetailHeaderProbe, {
      props: { title: 'MK4S' },
    });

    const root = container.querySelector('[data-slot="detail-header"]');
    expect(root?.className).toContain('@container/detail-header');
  });

  it('has nothing axe objects to', async () => {
    const { container } = await render(DetailHeaderProbe, {
      props: { title: 'MK4S', parts: true },
    });

    await expectNoViolations(container);
  });
});
