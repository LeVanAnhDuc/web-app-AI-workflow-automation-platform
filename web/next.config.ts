import type { NextConfig } from "next";

const apiOrigin = process.env.API_ORIGIN ?? "http://localhost:8080";

// The Go API is proxied through Next so the session cookie stays same-origin.
const nextConfig: NextConfig = {
  async rewrites() {
    return [
      { source: "/api/v1/:path*", destination: `${apiOrigin}/api/v1/:path*` },
      { source: "/webhook/:path*", destination: `${apiOrigin}/webhook/:path*` },
    ];
  },
};

export default nextConfig;
