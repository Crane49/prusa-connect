// Draws every mermaid diagram on the page, in the browser.
//
// The build leaves each diagram as a `figure.mermaid-figure` holding its
// source in a `pre` (see the rehype plugin in astro.config.mjs). This module
// replaces that `pre` with the drawing and keeps the source in the page,
// hidden, so a diagram mermaid refuses still reaches the reader as text.
//
// mermaid is bundled with the site and imported from it. Nothing is fetched
// from a CDN, so the site works offline and behind a policy that allows only
// its own origin.
import mermaid from 'mermaid';

// mermaid's own light and dark themes. Starlight writes the reader's choice
// into data-theme on the html element, and the system default resolves to one
// of the two before the page paints.
function themeFor(element) {
  return element.dataset.theme === 'dark' ? 'dark' : 'default';
}

// Each render needs an id no other element on the page holds. mermaid puts
// the id on the svg it builds and on the styles it scopes to it.
let counter = 0;

// The heading the diagram sits under, which names the scroll region the way
// the table regions of this site are named.
function headingAbove(figure) {
  for (let node = figure.previousElementSibling; node; node = node.previousElementSibling) {
    if (/^H[1-6]$/.test(node.tagName)) return node.textContent.trim();

    // Starlight wraps a heading and its anchor link in a div, so the heading
    // is rarely a sibling of the figure.
    const headings = node.querySelectorAll('h1, h2, h3, h4, h5, h6');

    if (headings.length > 0) return headings[headings.length - 1].textContent.trim();
  }

  return '';
}

// A diagram wider than the column it is read in has to scroll somewhere.
// mermaid's own answer is to shrink the drawing to the column, which on a
// phone puts the labels at a few pixels. The region scrolls instead, and the
// drawing keeps the size mermaid gave it.
function keepNaturalSize(svg) {
  const box = svg.getAttribute('viewBox');
  const width = box && Number(box.split(/\s+/)[2]);

  if (!width) return;

  svg.style.maxWidth = 'none';
  svg.style.width = `${width}px`;
}

async function draw(figure) {
  const source = figure.querySelector('.mermaid-source');
  const previous = figure.querySelector('.mermaid-drawing');

  if (!source) return;

  counter += 1;

  try {
    const { svg } = await mermaid.render(`mermaid-diagram-${counter}`, source.textContent.trim());
    const drawing = previous || document.createElement('div');

    drawing.className = 'mermaid-drawing';
    // The region is focusable and named, so a keyboard reaches what it
    // scrolls and a screen reader says which diagram was reached.
    drawing.setAttribute('role', 'region');
    drawing.setAttribute('tabindex', '0');
    drawing.setAttribute('aria-label', headingAbove(figure) || 'Diagram');
    drawing.innerHTML = svg;

    keepNaturalSize(drawing.querySelector('svg'));

    if (!previous) figure.prepend(drawing);

    source.hidden = true;
  } catch (error) {
    // The source stays visible. A diagram that does not parse is a defect in
    // the page, and docs/scripts/check-mermaid.mjs is the gate for it. The
    // console says which one and why.
    console.error('mermaid: this diagram did not render', error);

    if (previous) previous.remove();

    source.hidden = false;
  }
}

export async function renderDiagrams() {
  const figures = Array.from(document.querySelectorAll('figure.mermaid-figure'));

  if (figures.length === 0) return;

  const root = document.documentElement;

  const drawAll = async () => {
    mermaid.initialize({
      startOnLoad: false,
      theme: themeFor(root),
      // The strictest level mermaid has: it sanitises the diagram text and
      // renders no html label.
      securityLevel: 'strict',
      fontFamily: 'inherit',
    });

    for (const figure of figures) await draw(figure);
  };

  await drawAll();

  // The theme switch sets the attribute and reloads nothing, so the diagrams
  // are drawn again in the colours of the theme now in force.
  let drawn = themeFor(root);

  new MutationObserver(() => {
    const theme = themeFor(root);

    if (theme === drawn) return;

    drawn = theme;
    void drawAll();
  }).observe(root, { attributes: true, attributeFilter: ['data-theme'] });
}
