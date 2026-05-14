import type { Category } from './types';
import { cmd } from './cmd';
import { docker } from './docker';
import { linux } from './linux';
import { powershell } from './powershell';

export type { Category, Section, Example } from './types';

// Flat ordered list — sidebar grouping happens in the page component
// (groupBy `shell`).
export const CATEGORIES: Category[] = [...linux, ...powershell, ...cmd, ...docker];

export function getCategory(id: string): Category | undefined {
  return CATEGORIES.find((c) => c.id === id);
}
