import type { Category } from './types';

export const docker: Category[] = [
  {
    id: 'docker-containers',
    shell: 'Docker',
    label: 'Containers',
    icon: 'container',
    description: 'Day-to-day container operations: run, exec, logs, prune.',
    sections: [
      {
        id: 'run',
        title: 'docker run',
        examples: [
          { command: 'docker run -d --name web -p 8080:80 nginx:alpine', note: '-d detached, -p HOST:CONTAINER, --name pins a label.' },
          { command: 'docker run --rm -it ubuntu:24.04 bash', note: '--rm cleans up on exit, -it interactive TTY, bash drops you into a shell.' },
          { command: 'docker run -d --restart unless-stopped --name pihole -e TZ=Europe/Berlin -v pihole_data:/etc/pihole pihole/pihole', note: 'Realistic homelab service: env var, named volume, restart policy.' },
        ],
        tip: 'Volumes: `-v name:/path` for managed volumes (recommended), `-v /host/path:/container/path` for bind mounts.',
      },
      {
        id: 'exec',
        title: 'Inspect a running container',
        examples: [
          { command: 'docker ps', note: 'List running containers. -a includes stopped.' },
          { command: 'docker exec -it web sh', note: 'Drop into a shell inside the container.' },
          { command: 'docker logs -f --tail 200 web', note: 'Tail the last 200 lines and follow.' },
          { command: 'docker inspect web', note: 'Full JSON dump: env, mounts, network, IP.' },
          { command: 'docker stats', note: 'Live CPU/memory/network per container.' },
        ],
      },
      {
        id: 'cleanup',
        title: 'Cleanup',
        examples: [
          { command: 'docker container prune', note: 'Remove all stopped containers.' },
          { command: 'docker image prune -a', note: 'Remove all unused images (not just dangling).' },
          { command: 'docker volume prune', note: 'Remove unused volumes — ⚠️ also removes anonymous volumes that might still hold data.' },
          { command: 'docker system prune -a --volumes', note: 'Nuclear option. Reclaim everything not in use right now.' },
        ],
        warning: 'docker volume prune deletes anonymous volumes immediately. Named volumes are safe.',
      },
    ],
  },
  {
    id: 'docker-compose',
    shell: 'Docker',
    label: 'Compose',
    icon: 'layers',
    description: 'Multi-container apps defined in docker-compose.yml. Modern Docker uses `docker compose` (built-in subcommand) — older systems still have `docker-compose` as a separate binary.',
    sections: [
      {
        id: 'lifecycle',
        title: 'Start / stop / rebuild',
        examples: [
          { command: 'docker compose up -d', note: 'Create + start in detached mode. Re-runs are idempotent.' },
          { command: 'docker compose up -d --build', note: 'Rebuild images first. Use after editing a Dockerfile.' },
          { command: 'docker compose down', note: 'Stop and remove containers + default network.' },
          { command: 'docker compose down -v', note: '+ delete named volumes. Wipes data.' },
          { command: 'docker compose restart web', note: 'Restart one service.' },
        ],
      },
      {
        id: 'logs',
        title: 'Inspect',
        examples: [
          { command: 'docker compose logs -f', note: 'All services interleaved, tailing.' },
          { command: 'docker compose logs -f --tail 100 web', note: 'Last 100 lines of one service, then follow.' },
          { command: 'docker compose ps', note: 'List services + their states.' },
          { command: 'docker compose config', note: 'Print the merged, validated compose config.' },
        ],
      },
    ],
  },
  {
    id: 'docker-networks',
    shell: 'Docker',
    label: 'Networks',
    icon: 'network',
    description: 'Containers on the same user-defined network can address each other by service name.',
    sections: [
      {
        id: 'manage',
        title: 'Create + inspect',
        examples: [
          { command: 'docker network ls' },
          { command: 'docker network create app-net', note: 'Bridge driver by default — fine for most homelab setups.' },
          { command: 'docker network inspect app-net', note: 'See which containers are attached + their IPs.' },
          { command: 'docker run -d --network app-net --name db postgres:16', note: 'Attach a container at create time. Compose does this automatically.' },
        ],
      },
      {
        id: 'host-mode',
        title: 'Host networking',
        description: 'Container shares the host\'s network namespace. Required for things like LAN broadcast (Wake-on-LAN).',
        examples: [
          { command: 'docker run -d --network host --name labextend ghcr.io/bartis-dev/labextend:latest' },
        ],
        tip: 'Linux only. On Docker Desktop (Mac/Windows) host mode behaves differently — port mapping is the more portable choice when WoL isn\'t needed.',
      },
    ],
  },
];
