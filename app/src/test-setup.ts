Blob.prototype.text ??= function (this: Blob) {
  return new Promise<string>((resolve) => {
    const reader = new FileReader();
    reader.onload = () => resolve(reader.result as string);
    reader.readAsText(this);
  });
};
