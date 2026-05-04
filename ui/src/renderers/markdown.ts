import { marked } from "marked";
import DOMPurify from "dompurify";

marked.setOptions({
  gfm: true,
  breaks: true,
});

export function renderMarkdown(input: string): string {
  if (!input) return "";
  try {
    const html = marked.parse(input, { async: false }) as string;
    return DOMPurify.sanitize(html, { USE_PROFILES: { html: true } });
  } catch {
    return DOMPurify.sanitize(input, { USE_PROFILES: { html: true } });
  }
}
