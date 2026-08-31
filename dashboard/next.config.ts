import type { NextConfig } from "next";

const backendApiUrl = (process.env.BACKEND_API_URL ?? "http://api:8080").replace(/\/+$/, "");

const nextConfig: NextConfig = {
  output: "standalone",
  poweredByHeader: false,
  turbopack: { root: process.cwd() },
  experimental: { serverActions: { bodySizeLimit: "26mb" } },
  async rewrites() {
    return {
      beforeFiles: [
        {
          source: "/api/v1/:path*",
          destination: `${backendApiUrl}/api/v1/:path*`,
        },
        {
          source: "/internal/v1/:path*",
          destination: `${backendApiUrl}/internal/v1/:path*`,
        },
        {
          source: "/api/webhooks/v1/providers/:path*",
          destination: `${backendApiUrl}/webhooks/v1/providers/:path*`,
        },
        {
          source: "/webhooks/v1/providers/:path*",
          destination: `${backendApiUrl}/webhooks/v1/providers/:path*`,
        },
        {
          source: "/health/:path*",
          destination: `${backendApiUrl}/health/:path*`,
        },
      ],
      afterFiles: [],
      fallback: [],
    };
  },
  async headers() {
    return [{
      source: "/:path*",
      headers: [
        { key: "X-Content-Type-Options", value: "nosniff" },
        { key: "X-Frame-Options", value: "DENY" },
        { key: "Referrer-Policy", value: "no-referrer" },
        { key: "Permissions-Policy", value: "camera=(), microphone=(), geolocation=()" },
      ],
    }];
  },
};

export default nextConfig;
