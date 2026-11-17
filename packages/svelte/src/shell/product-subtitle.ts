/** Everything in `productName` after the first word; `Logo` draws the rest itself. */
export const productSubtitle = (productName: string): string =>
  productName.split(/\s+/u).slice(1).join(' ');
