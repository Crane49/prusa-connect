/** Hands a blob to the browser as a file download. */
export const saveBlob = (blob: Blob, name: string): void => {
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement('a');
  anchor.href = url;
  anchor.download = name;
  anchor.click();
  // Some browsers start reading the blob after the click returns.
  setTimeout(() => {
    URL.revokeObjectURL(url);
  }, 0);
};
