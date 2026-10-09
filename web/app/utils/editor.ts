// Textarea APIs normalize CRLF. Keep the original bytes for a no-op source save.
export const editorText = (source: string): string =>
  source.replace(/\r\n/g, "\n").replace(/\r/g, "\n");
export const savedSource = (original: string, current: string): string =>
  editorText(original) === current ? original : current;
export const previewDocument = (html: string): string =>
  `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'"><style>html{overflow:hidden}body{font:18px/1.7 system-ui,sans-serif;color:#1f2937;max-width:720px;margin:32px auto;padding:0 22px;overflow-wrap:anywhere}pre{overflow:auto;background:#f1f5f9;padding:16px}code{font-family:monospace}table{border-collapse:collapse}td,th{border:1px solid #cbd5e1;padding:8px}a{color:#37506d}</style></head><body>${html}</body></html>`;
