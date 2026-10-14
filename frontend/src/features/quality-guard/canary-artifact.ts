/** Extracts renderable markup from a canary answer (HTML document, or a bare <svg>). */
export function extractRenderableMarkup(text: string): string | null {
  const fenced = /```(?:html|svg|xml)?\s*([\s\S]*?)```/i.exec(text);
  const body = fenced ? fenced[1] : text;
  const lower = body.toLowerCase();
  const html = lower.indexOf("<html");
  if (html >= 0) {
    const end = lower.lastIndexOf("</html>");
    return end > html ? body.slice(html, end + 7) : body.slice(html);
  }
  const svg = lower.indexOf("<svg");
  const svgEnd = lower.lastIndexOf("</svg>");
  if (svg >= 0 && svgEnd > svg) return body.slice(svg, svgEnd + 6);
  return null;
}

const CSP = "default-src 'none'; style-src 'unsafe-inline'; img-src data:; font-src data:";

/**
 * Builds an isolated srcdoc: scripts, event handlers and external loads are
 * stripped, and a strict CSP is prepended. The iframe must also use an empty
 * sandbox attribute (no scripts, opaque origin).
 */
export function buildSandboxedDocument(markup: string): string {
  const cleaned = markup
    .replace(/<script[\s\S]*?<\/script\s*>/gi, "")
    .replace(/<script[^>]*>/gi, "")
    .replace(/<(iframe|object|embed|link|meta|base|form)\b[^>]*>/gi, "")
    .replace(/\son[a-z]+\s*=\s*("[^"]*"|'[^']*'|[^\s>]+)/gi, "")
    .replace(/(href|src|xlink:href)\s*=\s*("|')\s*(javascript|https?|\/\/)[^"']*\2/gi, "");
  return `<!doctype html><html><head><meta http-equiv="Content-Security-Policy" content="${CSP}"><style>html,body{margin:0;display:flex;align-items:center;justify-content:center;min-height:100%;background:#fff}svg{max-width:100%;max-height:100%}</style></head><body>${cleaned}</body></html>`;
}
