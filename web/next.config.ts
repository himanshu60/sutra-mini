import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  cacheComponents: true,
  partialPrefetching: true,
  redirects() {
    return [{ source: "/", destination: "/sync", permanent: false }];
  },
};

export default nextConfig;
