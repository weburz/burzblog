import sansUrl from "@fontsource-variable/sofia-sans/files/sofia-sans-latin-wght-normal.woff2?url";
import sansItalicUrl from "@fontsource-variable/sofia-sans/files/sofia-sans-latin-wght-italic.woff2?url";
import monoUrl from "@fontsource-variable/jetbrains-mono/files/jetbrains-mono-latin-wght-normal.woff2?url";

const escape = (text: string) =>
  text.replace(
    /[&<>"']/g,
    (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]!,
  );

const styles = `
@font-face{font-family:Sans;src:url(${sansUrl}) format("woff2");font-weight:100 900;font-style:normal}
@font-face{font-family:Sans;src:url(${sansItalicUrl}) format("woff2");font-weight:100 900;font-style:italic}
@font-face{font-family:Mono;src:url(${monoUrl}) format("woff2");font-weight:100 800}
:root{color-scheme:light}
html{-webkit-text-size-adjust:100%}
body{margin:0;background:#fff;color:#1f2937;font:18px/1.7 Sans,system-ui,sans-serif;overflow-wrap:anywhere}
main{max-width:680px;margin:0 auto;padding:40px 28px 96px}
header{margin-bottom:36px}
.cover{display:block;width:100%;aspect-ratio:1200/630;object-fit:cover;border-radius:8px;background:#f3f3ef;margin-bottom:28px}
h1.title{font-size:40px;line-height:1.1;letter-spacing:-.025em;font-weight:800;margin:0 0 14px}
h1.title.empty{color:#9aa5b1}
.lede{font-size:20px;line-height:1.5;color:#536478;margin:0}
article h1,article h2,article h3,article h4{line-height:1.25;letter-spacing:-.015em;margin:1.8em 0 .5em;font-weight:750}
article h1{font-size:32px}article h2{font-size:26px}article h3{font-size:21px}article h4{font-size:18px}
article p,article ul,article ol,article blockquote,article pre,article table{margin:0 0 1.15em}
article a{color:#37506d;text-decoration-thickness:1px;text-underline-offset:3px}
article img{max-width:100%;height:auto;border-radius:6px}
article blockquote{border-left:3px solid #fdd32a;margin-left:0;padding:2px 0 2px 20px;color:#536478}
article code{font:0.86em/1.5 Mono,ui-monospace,monospace;background:#f3f3ef;border-radius:4px;padding:.12em .35em}
article pre{background:#1f2937;color:#eaeef4;border-radius:8px;padding:18px 20px;overflow:auto}
article pre code{background:none;padding:0;color:inherit;font-size:14px}
article hr{border:0;border-top:1px solid #e4e6e9;margin:2.4em 0}
article table{border-collapse:collapse;display:block;overflow:auto;font-size:16px}
article th,article td{border:1px solid #e4e6e9;padding:8px 12px;text-align:left}
article th{background:#fafaf8}
article li{margin:.25em 0}
article input[type=checkbox]{margin-right:.5em}
.empty-body{color:#9aa5b1;font-style:italic}
.dark{color-scheme:dark}
.dark body{background:#182029;color:#e4e8ec}
.dark .cover,.dark article code{background:#1f2832}
.dark h1.title.empty,.dark .empty-body{color:#6d7987}
.dark .lede,.dark article blockquote{color:#aab5c1}
.dark article a{color:#a9c0d8}
.dark article pre{background:#0b0f14;color:#dfe5ec}
.dark article hr,.dark article th,.dark article td{border-color:#2b3541}
.dark article th{background:#1f2832}
`;

export interface PreviewMeta {
  title: string;
  description: string;
  coverUrl: string;
  coverAlt: string;
}

// The whole preview page for the sandboxed iframe: the sanitized article HTML
// from the server, laid out like a published post. The iframe allows no
// scripts; its own policy blocks everything but styles, fonts and images.
export const previewDocument = (
  html: string,
  meta: PreviewMeta,
  dark = false,
): string => {
  const cover = /^https?:\/\//.test(meta.coverUrl)
    ? `<img class="cover" src="${escape(meta.coverUrl)}" alt="${escape(meta.coverAlt)}">`
    : "";
  const title = meta.title.trim()
    ? `<h1 class="title">${escape(meta.title)}</h1>`
    : `<h1 class="title empty">Untitled</h1>`;
  const lede = meta.description.trim()
    ? `<p class="lede">${escape(meta.description)}</p>`
    : "";
  const body = html.trim() || `<p class="empty-body">Nothing written yet.</p>`;
  return `<!doctype html><html lang="en"${dark ? ' class="dark"' : ""}><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; font-src 'self'; img-src https: http: data:; base-uri 'none'; form-action 'none'"><base target="_blank"><style>${styles}</style></head><body><main><header>${cover}${title}${lede}</header><article>${body}</article></main></body></html>`;
};
