import type { Category } from './types';

export const cmd: Category[] = [
  {
    id: 'cmd-network',
    shell: 'cmd',
    label: 'Network (cmd.exe)',
    icon: 'network',
    description: 'Classic Windows network troubleshooting commands. PowerShell can do all of this too, but these one-liners survive in muscle memory.',
    sections: [
      {
        id: 'ipconfig',
        title: 'ipconfig',
        examples: [
          { command: 'ipconfig /all', note: 'Full info: MAC, DHCP server, DNS, lease.' },
          { command: 'ipconfig /flushdns', note: 'Clear the resolver cache. The classic "DNS changed but my browser doesn\'t see it" fix.' },
          { command: 'ipconfig /release && ipconfig /renew', note: 'Force a fresh DHCP lease.' },
        ],
      },
      {
        id: 'netstat',
        title: 'netstat',
        examples: [
          { command: 'netstat -ano', note: '-a all, -n numeric, -o owning PID. Combine with `findstr` to search.' },
          { command: 'netstat -ano | findstr LISTENING', note: 'All listening sockets.' },
          { command: 'netstat -ano | findstr :443', note: 'Who\'s on port 443?' },
        ],
      },
    ],
  },
  {
    id: 'cmd-files',
    shell: 'cmd',
    label: 'Files & system',
    icon: 'folder',
    sections: [
      {
        id: 'robocopy',
        title: 'robocopy',
        description: 'Robust copy. Built-in, fast, scriptable. Use this — not `xcopy`.',
        examples: [
          { command: 'robocopy C:\\src D:\\dst /E /MT', note: '/E recursive (incl. empty dirs), /MT multithreaded.' },
          { command: 'robocopy C:\\src D:\\dst /MIR', note: '⚠️ Mirror mode — DELETES files in dst that don\'t exist in src.' },
          { command: 'robocopy C:\\src D:\\dst /E /LOG:copy.log /TEE', note: 'Log to file AND console.' },
        ],
        warning: '/MIR is destructive. Always test with /L (list-only, dry run) first if you\'re uncertain.',
      },
      {
        id: 'sfc',
        title: 'sfc / DISM — Windows file integrity',
        examples: [
          { command: 'sfc /scannow', note: 'Verify and repair Windows system files. Run elevated.' },
          { command: 'DISM /Online /Cleanup-Image /RestoreHealth', note: 'Repairs the component store that sfc draws from. Use when sfc finds errors it can\'t fix.' },
        ],
      },
    ],
  },
];
