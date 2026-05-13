import type { Shell } from './types';

export const powershell: Shell = {
  id: 'powershell',
  label: 'PowerShell',
  description: 'Windows PowerShell / PowerShell 7 cmdlets and patterns.',
  icon: 'terminal',
  commands: [
    {
      id: 'get-childitem',
      name: 'Get-ChildItem',
      category: 'Filesystem',
      description: 'List items in a path (PowerShell\'s ls / dir).',
      template: 'Get-ChildItem {flags} -Path {path}',
      args: [
        { key: 'path', name: 'Path', description: 'Directory to list.', required: true, placeholder: 'C:\\Users' },
      ],
      flags: [
        { key: 'recurse', flag: '-Recurse', description: 'Descend into subdirectories.', type: 'bool' },
        { key: 'force', flag: '-Force', description: 'Include hidden/system items.', type: 'bool' },
        { key: 'filter', flag: '-Filter', description: 'Provider-level wildcard filter.', type: 'string', placeholder: '*.log' },
        { key: 'directory', flag: '-Directory', description: 'Only directories.', type: 'bool' },
        { key: 'file', flag: '-File', description: 'Only files.', type: 'bool' },
      ],
      examples: ['Get-ChildItem -Path C:\\Logs -Recurse -Filter *.log'],
    },
    {
      id: 'get-service',
      name: 'Get-Service',
      category: 'Services',
      description: 'Show services (and their state).',
      template: 'Get-Service {flags} {name}',
      args: [
        { key: 'name', name: 'Name', description: 'Service name or wildcard.', placeholder: 'Spool*' },
      ],
      flags: [
        { key: 'computer', flag: '-ComputerName', description: 'Remote computer.', type: 'string', placeholder: 'server01' },
      ],
      examples: ['Get-Service Spool*'],
    },
    {
      id: 'set-service',
      name: 'Set-Service',
      category: 'Services',
      description: 'Change a service\'s startup type or state.',
      template: 'Set-Service -Name {name} {flags}',
      args: [
        { key: 'name', name: 'Name', description: 'Service name.', required: true, placeholder: 'Spooler' },
      ],
      flags: [
        { key: 'startup', flag: '-StartupType', description: 'Startup type.', type: 'enum', options: ['Automatic', 'Manual', 'Disabled', 'AutomaticDelayedStart'] },
        { key: 'status', flag: '-Status', description: 'Desired state.', type: 'enum', options: ['Running', 'Stopped', 'Paused'] },
      ],
      examples: ['Set-Service -Name Spooler -StartupType Disabled -Status Stopped'],
    },
    {
      id: 'test-netconnection',
      name: 'Test-NetConnection',
      category: 'Network',
      description: 'Test reachability of a host (PS replacement for ping + telnet).',
      template: 'Test-NetConnection {host} {flags}',
      args: [
        { key: 'host', name: 'Host', description: 'Hostname or IP.', required: true, placeholder: 'example.com' },
      ],
      flags: [
        { key: 'port', flag: '-Port', description: 'TCP port to probe.', type: 'number', placeholder: '443' },
        { key: 'trace', flag: '-TraceRoute', description: 'Trace the route.', type: 'bool' },
      ],
      examples: ['Test-NetConnection example.com -Port 443'],
    },
    {
      id: 'invoke-restmethod',
      name: 'Invoke-RestMethod',
      category: 'HTTP',
      description: 'HTTP client that returns parsed objects (curl + jq in one).',
      template: 'Invoke-RestMethod {flags} -Uri {uri}',
      args: [
        { key: 'uri', name: 'URI', description: 'Target URL.', required: true, placeholder: 'https://api.example.com/users' },
      ],
      flags: [
        { key: 'method', flag: '-Method', description: 'HTTP method.', type: 'enum', options: ['GET', 'POST', 'PUT', 'DELETE', 'PATCH'], default: 'GET' },
        { key: 'headers', flag: '-Headers', description: 'Hashtable of headers (e.g. @{Auth="Bearer ..."}).', type: 'string', placeholder: '@{Authorization="Bearer ..."}' },
        { key: 'body', flag: '-Body', description: 'Request body.', type: 'string' },
        { key: 'contenttype', flag: '-ContentType', description: 'Content-Type.', type: 'string', placeholder: 'application/json' },
      ],
      examples: ['Invoke-RestMethod -Uri https://api.example.com/users -Method POST -Body $json -ContentType application/json'],
    },
    {
      id: 'get-process',
      name: 'Get-Process',
      category: 'Processes',
      description: 'List running processes.',
      template: 'Get-Process {name}',
      args: [
        { key: 'name', name: 'Name', description: 'Optional name filter.', placeholder: 'chrome' },
      ],
      examples: ['Get-Process | Sort-Object CPU -Descending | Select-Object -First 10'],
    },
  ],
  paths: [
    { path: '$PROFILE', description: 'PowerShell profile script (run at startup).', category: 'Profile' },
    { path: '$env:PSModulePath', description: 'Module search paths.', category: 'Modules' },
    { path: '$env:USERPROFILE', description: 'Current user\'s home directory.', category: 'Variables' },
    { path: 'C:\\Windows\\System32\\WindowsPowerShell\\v1.0\\', description: 'Built-in Windows PowerShell 5.1 install.', category: 'Install' },
    { path: 'C:\\Program Files\\PowerShell\\7\\', description: 'PowerShell 7 install (cross-platform).', category: 'Install' },
  ],
};
