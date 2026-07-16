import type { NextConfig } from "next";

const isExport = process.env.EXPORT_STATIC === "true";

const nextConfig: NextConfig = {
  ...(isExport ? { output: "export" as const } : {}),
  images: { unoptimized: true },
  ...(!isExport
    ? {
        async rewrites() {
          return [
            { source: "/api/:path*", destination: "http://localhost:8081/api/:path*" },
            { source: "/sse", destination: "http://localhost:8081/sse" },
            {
              source: "/mcp",
              destination: "http://localhost:8081/",
              has: [{ type: "header", key: "content-type", value: "application/json" }],
            },
          ];
        },
      }
    : {}),
};

export default nextConfig;
