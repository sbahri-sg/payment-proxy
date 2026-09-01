import { llmsContent } from "../llms-content";

export function GET() {
  return new Response(llmsContent, {
    headers: {
      "Cache-Control": "public, max-age=300",
      "Content-Type": "text/plain; charset=utf-8",
      "X-Content-Type-Options": "nosniff",
    },
  });
}
