import type { Category } from './types';

export const linux: Category[] = [
  {
    id: 'linux-permissions',
    shell: 'Linux',
    label: 'Permissions',
    icon: 'shield',
    description: 'Who can read, write, or execute a file. Three classes (User, Group, Other) × three bits (r=4, w=2, x=1).',
    sections: [
      {
        id: 'chmod',
        title: 'chmod — change file mode',
        description: 'Numeric (octal) form is the easiest. Each digit is the sum of bits for one class.',
        examples: [
          { command: 'chmod 755 /srv/app', note: 'rwxr-xr-x: owner full, others read+execute. Standard for binaries and directories.' },
          { command: 'chmod 644 /etc/nginx/sites-available/site.conf', note: 'rw-r--r--: standard for config files.' },
          { command: 'chmod 600 ~/.ssh/id_ed25519', note: 'rw-------: required for private SSH keys (sshd refuses anything looser).' },
          { command: 'chmod -R 750 /var/lib/myapp', note: '-R recurses; useful for app-data dirs.' },
          { command: 'chmod u+x deploy.sh', note: 'symbolic form: add execute for the user (owner) only.' },
        ],
        tip: 'Common modes cheat-sheet → 755 binaries/dirs · 644 regular files · 600 private files · 700 private dirs · 750 group-shared.',
      },
      {
        id: 'chown',
        title: 'chown — change owner / group',
        examples: [
          { command: 'chown bartis:users /srv/app/data.json' },
          { command: 'chown -R www-data:www-data /var/www/html', note: 'Recursive ownership change for a webroot.' },
          { command: 'chown :docker docker-compose.yml', note: 'Group-only change (omit the user).' },
        ],
      },
      {
        id: 'umask',
        title: 'umask — default permission mask',
        description: 'Bits stripped from the default mode (666 for files, 777 for dirs) when something is created.',
        examples: [
          { command: 'umask', note: 'Show current mask.' },
          { command: 'umask 027', note: 'New files = 640, new dirs = 750. Common in shared-server profile.' },
        ],
        tip: 'Set system-wide in /etc/profile or per-user in ~/.bashrc / ~/.zshrc.',
      },
      {
        id: 'special-bits',
        title: 'Sticky bit, setuid, setgid',
        description: 'Special permission bits beyond rwx.',
        examples: [
          { command: 'chmod +t /tmp', note: 'Sticky: only the owner can delete files in this directory (already on /tmp by default).' },
          { command: 'chmod g+s /srv/shared', note: 'setgid on a dir: new files inherit the dir\'s group.' },
          { command: 'chmod u+s /usr/bin/passwd', note: 'setuid on a binary: runs as the owner regardless of caller. ⚠️ security-sensitive.' },
        ],
      },
    ],
  },

  {
    id: 'linux-networking',
    shell: 'Linux',
    label: 'Networking',
    icon: 'network',
    description: 'Inspect interfaces, routes, sockets, DNS. Modern systems use the iproute2 family (ip, ss).',
    sections: [
      {
        id: 'ip',
        title: 'ip — interfaces, addresses, routes',
        examples: [
          { command: 'ip addr show', note: 'List all interfaces and their IPs. Short form: `ip a`.' },
          { command: 'ip route', note: 'Show the routing table. Default route shown as `default via …`.' },
          { command: 'ip link set eth0 up', note: 'Bring an interface up.' },
          { command: 'ip addr add 192.168.1.50/24 dev eth0', note: 'Add a static IP (volatile — use NetworkManager / netplan / systemd-networkd to persist).' },
        ],
      },
      {
        id: 'ss',
        title: 'ss — open sockets',
        description: 'Successor to netstat. Faster and reads from /proc/net/tcp directly.',
        examples: [
          { command: 'ss -tlnp', note: 'TCP, listening, numeric, with PID. The classic "what\'s on this port?" question.' },
          { command: 'ss -tunap | grep 8080', note: 'TCP+UDP, all states, with process — find a process by port.' },
          { command: 'ss -s', note: 'Summary: total sockets per protocol.' },
        ],
      },
      {
        id: 'dns',
        title: 'DNS lookups',
        examples: [
          { command: 'dig example.com', note: 'Authoritative DNS query. Add `+short` for just the answer.' },
          { command: 'dig +short MX example.com', note: 'Specific record type, short output.' },
          { command: 'getent hosts example.com', note: 'Resolve via the system\'s NSS chain (uses /etc/hosts + nsswitch.conf).' },
          { command: 'systemd-resolve --status', note: 'Show systemd-resolved state and per-link DNS servers.' },
        ],
      },
      {
        id: 'firewall',
        title: 'Firewall — ufw / nftables',
        examples: [
          { command: 'ufw status verbose', note: 'Current firewall state.' },
          { command: 'ufw allow 22/tcp', note: 'Allow SSH.' },
          { command: 'ufw deny from 1.2.3.4', note: 'Block a specific source IP.' },
          { command: 'nft list ruleset', note: 'Dump the entire nftables ruleset.' },
        ],
        tip: 'On Debian/Ubuntu, ufw is a friendly front-end for nftables (or iptables). Make sure it\'s enabled (`ufw enable`) before relying on it.',
      },
    ],
    // Important paths shown at the bottom of the page.
  },

  {
    id: 'linux-users',
    shell: 'Linux',
    label: 'Users & sudo',
    icon: 'users',
    description: 'Account management and privilege escalation. Most operations require root.',
    sections: [
      {
        id: 'useradd',
        title: 'Create users',
        examples: [
          { command: 'useradd -m -s /bin/bash bartis', note: '-m creates a home directory; -s sets the login shell.' },
          { command: 'passwd bartis', note: 'Set or change the password.' },
          { command: 'usermod -aG docker,sudo bartis', note: 'Append (-a) the user to additional groups (-G). Without -a, you replace the group list.' },
          { command: 'id bartis', note: 'Show UID, primary GID, and supplementary groups.' },
        ],
        warning: 'On most systems you must log out and back in (or run `newgrp <group>`) for new group membership to take effect.',
      },
      {
        id: 'sudo',
        title: 'sudo — controlled privilege escalation',
        examples: [
          { command: 'sudo -i', note: 'Open an interactive root shell.' },
          { command: 'sudo -u www-data ls /var/www', note: 'Run a command as a specific user (not root).' },
          { command: 'visudo', note: 'Safely edit the sudoers file (validates syntax before saving).' },
          { command: 'visudo -f /etc/sudoers.d/myrules', note: 'Edit a drop-in file — preferred over editing /etc/sudoers directly.' },
        ],
        tip: 'sudoers line format: `bartis ALL=(ALL:ALL) NOPASSWD: /usr/bin/systemctl restart nginx` — gives one user passwordless access to one specific command.',
      },
      {
        id: 'ssh-keys',
        title: 'SSH keys',
        examples: [
          { command: 'ssh-keygen -t ed25519 -C "me@example.com"', note: 'Generate a modern Ed25519 keypair.' },
          { command: 'ssh-copy-id user@host', note: 'Append your public key to the remote ~/.ssh/authorized_keys.' },
          { command: 'ssh-add ~/.ssh/id_ed25519', note: 'Load a key into the SSH agent for the session.' },
        ],
      },
    ],
  },

  {
    id: 'linux-services',
    shell: 'Linux',
    label: 'Services (systemd)',
    icon: 'cpu',
    description: 'systemd is the init + service manager on essentially every modern Linux. Units live in /etc/systemd/system/ and /usr/lib/systemd/system/.',
    sections: [
      {
        id: 'systemctl-basics',
        title: 'Start, stop, enable',
        examples: [
          { command: 'systemctl status nginx', note: 'Detailed status + recent log lines.' },
          { command: 'systemctl start nginx', note: 'Start once.' },
          { command: 'systemctl restart nginx' },
          { command: 'systemctl enable --now docker', note: 'Enable at boot AND start now in one command.' },
          { command: 'systemctl disable docker', note: 'Don\'t auto-start at boot (already running services keep running).' },
        ],
      },
      {
        id: 'list-units',
        title: 'List units / find a service',
        examples: [
          { command: 'systemctl list-units --type service --state running' },
          { command: 'systemctl list-units --type service --state failed', note: 'The "what broke?" command.' },
          { command: 'systemctl list-unit-files --type service', note: 'Include inactive units (everything that exists, not just loaded).' },
        ],
      },
      {
        id: 'edit-units',
        title: 'Edit / override units',
        description: 'Don\'t touch the distro-shipped unit files in /usr/lib/systemd/system/. Use overrides.',
        examples: [
          { command: 'systemctl edit nginx', note: 'Opens an empty drop-in (data lands in /etc/systemd/system/nginx.service.d/override.conf).' },
          { command: 'systemctl edit --full custom.service', note: 'Edit the FULL unit (creates a copy in /etc/ that masks the distro one).' },
          { command: 'systemctl daemon-reload', note: 'Required after editing any unit file. Then restart the service for the changes to take effect.' },
        ],
        tip: 'Drop-in override file format: write only the [Section] and key=value lines you want to change. systemd merges yours on top.',
      },
      {
        id: 'journalctl',
        title: 'journalctl — read the log',
        examples: [
          { command: 'journalctl -u nginx -f', note: 'Tail (-f) the journal for one unit. Cancel with Ctrl-C.' },
          { command: 'journalctl --since "1 hour ago"', note: 'Time-based filter. Also accepts ISO timestamps.' },
          { command: 'journalctl -p err -b', note: '-p err = only errors (and worse), -b = since the current boot.' },
          { command: 'journalctl --disk-usage', note: 'How much space is the journal eating?' },
        ],
      },
    ],
  },

  {
    id: 'linux-filesystem',
    shell: 'Linux',
    label: 'Filesystem',
    icon: 'hard-drive',
    description: 'Mounts, layout, disk usage. fstab persists, mount is for runtime.',
    sections: [
      {
        id: 'mount',
        title: 'Mount / unmount',
        examples: [
          { command: 'mount', note: 'Show currently mounted filesystems.' },
          { command: 'mount /dev/sdb1 /mnt/usb', note: 'One-shot mount (lost at reboot). Use fstab for persistence.' },
          { command: 'umount /mnt/usb', note: 'Detach. Add -l for "lazy" if it\'s busy.' },
          { command: 'findmnt /', note: 'Pretty tree of mountpoints.' },
        ],
      },
      {
        id: 'fstab',
        title: '/etc/fstab — persistent mounts',
        description: 'Read at boot. Each line: device  mountpoint  fstype  options  dump  pass.',
        examples: [
          { command: 'UUID=abcd-1234  /mnt/data  ext4  defaults,noatime  0  2', note: 'Use UUID (from `blkid`) instead of /dev/sdX — device names can shift.' },
          { command: 'systemctl daemon-reload && mount -a', note: 'After editing fstab. -a mounts everything in fstab not already mounted.' },
        ],
        warning: 'A bad fstab can prevent boot. Test with `mount -a` before rebooting.',
      },
      {
        id: 'disk-usage',
        title: 'Disk usage',
        examples: [
          { command: 'df -hT', note: '-h human, -T type. Quick "what\'s mounted and full?" view.' },
          { command: 'du -sh *', note: 'Size of every entry in the current directory.' },
          { command: 'du -sh * | sort -h', note: 'Sorted ascending. Pipe through `tail` for the biggest.' },
          { command: 'ncdu /var', note: 'Interactive disk-usage browser. Install separately on most distros.' },
        ],
      },
    ],
  },

  {
    id: 'linux-processes',
    shell: 'Linux',
    label: 'Processes',
    icon: 'activity',
    description: 'Find, signal, and inspect running processes.',
    sections: [
      {
        id: 'ps',
        title: 'ps — list processes',
        examples: [
          { command: 'ps aux', note: 'BSD-style: all processes, all users, with command lines.' },
          { command: 'ps aux | grep nginx', note: 'Quick filter (also matches the grep itself — use `grep -v grep` to remove).' },
          { command: 'pgrep -fl nginx', note: 'Cleaner: list PIDs and command of matching processes.' },
        ],
      },
      {
        id: 'top',
        title: 'top / htop',
        examples: [
          { command: 'top', note: 'Built-in. Press P to sort by CPU, M by memory, q to quit.' },
          { command: 'htop', note: 'Friendlier interactive top. Install separately.' },
        ],
      },
      {
        id: 'kill',
        title: 'Send signals',
        examples: [
          { command: 'kill 12345', note: 'SIGTERM by default — asks the process to exit cleanly.' },
          { command: 'kill -9 12345', note: 'SIGKILL — ungraceful, last resort. Process can\'t catch this.' },
          { command: 'pkill -f my-app', note: 'Match by command line instead of PID.' },
          { command: 'killall nginx', note: 'Kill every process named nginx.' },
        ],
      },
    ],
  },
];
