import type { NextConfig } from "next";

const isExport = process.env.EXPORT_STATIC === "true";

const nextConfig: NextConfig = {
  ...(isExport ? { output: "export" as const } : {}),
  images: { unoptimized: true },
  ...(!isExport
    ? {
        async rewrites() {
          return [
            { source: "/api/tables/:name/items", destination: "http://localhost:8081/api/tables/:name/items" },
            { source: "/api/tables/:name", destination: "http://localhost:8081/api/tables/:name" },
            { source: "/api/tables", destination: "http://localhost:8081/api/tables" },
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
