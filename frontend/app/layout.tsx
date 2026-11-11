import type { Metadata, Viewport } from 'next';
import './globals.css';
import { Toaster } from '@/components/toast';
import { ConfirmHost } from '@/components/confirm';

export const metadata: Metadata = {
  title: 'LabExtend',
  description: 'Docker Swarm infrastructure manager',
};

export const viewport: Viewport = {
  width: 'device-width',
  initialScale: 1,
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" suppressHydrationWarning>
      <body className="min-h-screen">
        {children}
        <Toaster />
        <ConfirmHost />
      </body>
    </html>
  );
}
