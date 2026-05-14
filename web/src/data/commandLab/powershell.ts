import type { Category } from './types';

export const powershell: Category[] = [
  {
    id: 'powershell-files',
    shell: 'PowerShell',
    label: 'Files & filesystem',
    icon: 'folder',
    description: 'PowerShell cmdlets for the typical "ls / cd / cat / find" tasks. PowerShell pipes objects, not text.',
    sections: [
      {
        id: 'navigate',
        title: 'Navigate',
        examples: [
          { command: 'Get-ChildItem -Recurse -Filter *.log C:\\Logs', note: 'List with filter + recurse. Aliases: `ls`, `dir`, `gci`.' },
          { command: 'Get-Content -Tail 50 -Wait C:\\Logs\\app.log', note: 'tail -f equivalent.' },
          { command: 'Set-Location C:\\Users\\Bartis', note: 'cd. Aliases: `cd`, `sl`.' },
        ],
      },
      {
        id: 'copy-move',
        title: 'Copy, move, delete',
        examples: [
          { command: 'Copy-Item -Recurse C:\\src C:\\dst' },
          { command: 'Move-Item old.txt archive\\' },
          { command: 'Remove-Item -Recurse -Force C:\\temp\\old', note: '⚠️ -Force bypasses confirmation. Double-check the path.' },
        ],
      },
    ],
  },
  {
    id: 'powershell-services',
    shell: 'PowerShell',
    label: 'Services & processes',
    icon: 'cpu',
    sections: [
      {
        id: 'services',
        title: 'Get-Service / Set-Service',
        examples: [
          { command: 'Get-Service -Name Spool*', note: 'Filter by wildcard.' },
          { command: 'Restart-Service -Name Spooler' },
          { command: 'Set-Service -Name Spooler -StartupType Automatic -Status Running' },
          { command: 'Get-Service | Where-Object Status -eq Stopped | Where-Object StartType -eq Automatic', note: 'Find services that should be running but aren\'t.' },
        ],
      },
      {
        id: 'processes',
        title: 'Get-Process / Stop-Process',
        examples: [
          { command: 'Get-Process | Sort-Object CPU -Descending | Select-Object -First 10', note: 'Top 10 by CPU.' },
          { command: 'Stop-Process -Name notepad -Force' },
        ],
      },
    ],
  },
  {
    id: 'powershell-network',
    shell: 'PowerShell',
    label: 'Network',
    icon: 'network',
    sections: [
      {
        id: 'connectivity',
        title: 'Reachability + open ports',
        examples: [
          { command: 'Test-NetConnection example.com -Port 443', note: 'PowerShell\'s ping + telnet replacement.' },
          { command: 'Test-NetConnection example.com -TraceRoute' },
          { command: 'Get-NetIPAddress -AddressFamily IPv4', note: 'Local IP configuration.' },
          { command: 'Resolve-DnsName example.com -Type MX' },
        ],
      },
      {
        id: 'http',
        title: 'HTTP requests',
        examples: [
          { command: 'Invoke-RestMethod -Uri https://api.example.com/users', note: 'Returns parsed objects (JSON → PSObject).' },
          { command: 'Invoke-RestMethod -Uri https://api.example.com/users -Method POST -Body $json -ContentType application/json' },
          { command: 'Invoke-WebRequest -Uri https://example.com -OutFile out.html', note: 'Use Invoke-WebRequest when you need raw response bytes / status codes.' },
        ],
      },
    ],
  },
];
