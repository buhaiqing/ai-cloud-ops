/** @type {import('next').NextConfig} */
const nextConfig = {
  reactStrictMode: true,
  // Reverse-proxy API calls to the Go backend.
  // Set AICO_BACKEND_URL in .env.local to override.
  async rewrites() {
    const backend = process.env.AICO_BACKEND_URL || 'http://localhost:8080';
    return [{ source: '/api/v1/:path*', destination: `${backend}/api/v1/:path*` }];
  },
};
module.exports = nextConfig;