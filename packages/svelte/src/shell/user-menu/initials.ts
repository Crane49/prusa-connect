/** `user:alice` gives `AL`; a single-part subject gives its own first two letters. */
export function subjectInitials(subject: string): string {
  const withoutKind = subject.slice(subject.indexOf(':') + 1);
  const parts = withoutKind.split(/[\s@._-]+/).filter((part) => part !== '');

  if (parts.length === 0) {
    return '';
  }

  if (parts.length === 1) {
    return (parts[0] ?? '').slice(0, 2).toUpperCase();
  }

  return parts
    .slice(0, 2)
    .map((part) => part.charAt(0))
    .join('')
    .toUpperCase();
}
