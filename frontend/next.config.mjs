/** @type {import('next').NextConfig} */
const nextConfig = {
  // Pure static export → consumed by the Go binary via embed.FS.
  output: 'export',
  // Keep route URLs hyphenated/no-trailing-slash so they match the Go SPA
  // fallback behaviour cleanly.
  trailingSlash: false,
  // Disable Image optimization since we're shipping a static site.
  images: { unoptimized: true },
  // The leader serves the SPA from /, so no base path needed.
};
export default nextConfig;
