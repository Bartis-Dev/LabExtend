import type { Shell } from './types';
import { cmd } from './cmd';
import { docker } from './docker';
import { linux } from './linux';
import { powershell } from './powershell';

export { type Shell, type Command, type Flag, type Argument, type FilePath } from './types';

export const SHELLS: Shell[] = [linux, powershell, cmd, docker];

export function getShell(id: string): Shell | undefined {
  return SHELLS.find((s) => s.id === id);
}
