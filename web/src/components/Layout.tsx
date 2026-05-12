import type { ReactNode } from 'react';
import { Navbar } from './Navbar';

export function Layout({ children }: { children: ReactNode }) {
  return (
    <div className="flex h-full flex-col">
      <Navbar />
      <main className="flex-1 overflow-y-auto">{children}</main>
      <Footer />
    </div>
  );
}

function Footer() {
  return (
    <footer className="border-t border-border/40 px-6 py-2 text-center text-[11px] text-fg-muted/60">
      <a
        href="https://bartis.me"
        target="_blank"
        rel="noopener noreferrer"
        className="transition-colors hover:text-fg-muted"
      >
        bartis.me
      </a>
    </footer>
  );
}
