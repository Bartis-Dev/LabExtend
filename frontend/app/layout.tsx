import type { Metadata } from 'next';
import './globals.css';

export const metadata: Metadata = {
  title: 'labextend',
  description: 'Docker Swarm infrastructure manager',
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" suppressHydrationWarning>
      <body className="min-h-screen bg-zinc-50 text-zinc-900 antialiased dark:bg-zinc-950 dark:text-zinc-100">
        {/* TODO(phase 5): wrap with <ThemeProvider> + <SseProvider> + toast system. */}
        {children}
      </body>
    </html>
  );
}
