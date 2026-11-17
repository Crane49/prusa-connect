// The colour families a list narrows to. The module matches `color` against a
// colour family, so a swatch finds every shade of its name; the hex is a
// typical colour of the family, for the swatch.
export const PALETTE = [
  { key: 'black', hex: '#1a1a1a' },
  { key: 'white', hex: '#ffffff' },
  { key: 'grey', hex: '#808080' },
  { key: 'red', hex: '#ff0000' },
  { key: 'orange', hex: '#ff8000' },
  { key: 'yellow', hex: '#ffff00' },
  { key: 'green', hex: '#00ff00' },
  { key: 'blue', hex: '#0000ff' },
  { key: 'purple', hex: '#8000ff' },
  { key: 'pink', hex: '#ffc0cb' },
  { key: 'brown', hex: '#8b4513' },
  { key: 'natural', hex: '#f5f0dc' },
] as const;
