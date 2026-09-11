#!/usr/bin/env bash
# gen-mtls.sh — generate a self-signed CA + leader + agent certs for the
# labextend gRPC channel.
#
# By default the leader-agent gRPC uses a shared-secret token in metadata.
# That's fine for a private overlay network but doesn't authenticate the
# server. Adding mTLS gives:
#   • the leader proves its identity to agents
#   • agents prove their identity to the leader
#   • the channel itself is encrypted (gRPC over TLS rather than h2c)
#
# Usage:
#   bash scripts/gen-mtls.sh ./mtls-out
#
# Output:
#   ./mtls-out/ca.crt           — root cert to ship to every node
#   ./mtls-out/leader.crt       — leader server cert
#   ./mtls-out/leader.key
#   ./mtls-out/<node>.crt       — one cert per agent node, CN = its BPM_AGENT_HOST_ID
#   ./mtls-out/<node>.key
#
# Pass the agent node IDs as arguments after the output dir, or via AGENT_IDS:
#   bash scripts/gen-mtls.sh ./mtls-out node-a node-b node-c
#
# One cert PER NODE is deliberate. The leader binds the claimed "agent-id" to
# the client certificate's CN, so a shared agent cert would let every node
# impersonate every other one — taking over its registry slot and receiving the
# S3 credentials the leader decrypts for that node's backups.
#
# After generating, mount them into both leader and agents and set:
#   leader: BPM_GRPC_TLS_CERT=/run/secrets/leader.crt
#           BPM_GRPC_TLS_KEY=/run/secrets/leader.key
#           BPM_GRPC_TLS_CLIENT_CA=/run/secrets/ca.crt
#   agent:  BPM_GRPC_TLS_CLIENT_CA=/run/secrets/ca.crt
#           BPM_GRPC_TLS_CERT=/run/secrets/<node>.crt
#           BPM_GRPC_TLS_KEY=/run/secrets/<node>.key
#           BPM_AGENT_HOST_ID=<node>            # must equal the cert CN

set -euo pipefail

OUT="${1:-mtls-out}"
shift || true
# Agent node IDs: positional args win, else AGENT_IDS, else a single default.
if [[ $# -gt 0 ]]; then
  AGENTS=("$@")
else
  IFS=',' read -ra AGENTS <<<"${AGENT_IDS:-labextend-agent}"
fi
LEADER_HOSTS="${LEADER_HOSTS:-labextend-leader,localhost}"
DAYS=3650 # 10 years for the CA; rotate before then

mkdir -p "$OUT"
cd "$OUT"

if [[ ! -f ca.key ]]; then
  echo "[mtls] generating CA …"
  openssl genrsa -out ca.key 4096
  openssl req -new -x509 -days $DAYS -key ca.key -out ca.crt \
    -subj "/CN=labextend-ca"
fi

gen_cert() {
  local name=$1
  local subj_cn=$2
  local sans=$3
  echo "[mtls] generating $name cert (CN=$subj_cn, SAN=$sans)…"
  openssl genrsa -out "$name.key" 2048
  cat > "$name.ext" <<EOF
authorityKeyIdentifier=keyid,issuer
basicConstraints=CA:FALSE
keyUsage = digitalSignature, nonRepudiation, keyEncipherment, dataEncipherment
extendedKeyUsage = serverAuth, clientAuth
subjectAltName = $sans
EOF
  openssl req -new -key "$name.key" -out "$name.csr" -subj "/CN=$subj_cn"
  openssl x509 -req -in "$name.csr" -CA ca.crt -CAkey ca.key -CAcreateserial \
    -out "$name.crt" -days 825 -sha256 -extfile "$name.ext"
  rm -f "$name.csr" "$name.ext"
}

# Build SAN for leader from comma-separated hostnames.
LEADER_SAN=""
IFS=',' read -ra HS <<<"$LEADER_HOSTS"
for h in "${HS[@]}"; do
  LEADER_SAN+="DNS:${h},"
done
LEADER_SAN="${LEADER_SAN%,}"

gen_cert leader "labextend-leader" "$LEADER_SAN"

# CN must equal the node's BPM_AGENT_HOST_ID — the leader rejects a mismatch.
for a in "${AGENTS[@]}"; do
  gen_cert "$a" "$a" "DNS:${a}"
done

echo
echo "[mtls] done. Output in: $OUT"
echo "       ca.crt, leader.crt+key, and one crt+key per agent: ${AGENTS[*]}"
echo
echo "Create Docker secrets:"
echo "  docker secret create labextend-ca-crt     $OUT/ca.crt"
echo "  docker secret create labextend-leader-crt $OUT/leader.crt"
echo "  docker secret create labextend-leader-key $OUT/leader.key"
for a in "${AGENTS[@]}"; do
  echo "  docker secret create labextend-${a}-crt   $OUT/${a}.crt"
  echo "  docker secret create labextend-${a}-key   $OUT/${a}.key"
done
echo
echo "Each agent must run with BPM_AGENT_HOST_ID equal to its cert CN."
