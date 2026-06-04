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
#   ./mtls-out/agent.crt        — shared agent cert (all agents use the same)
#   ./mtls-out/agent.key
#
# After generating, mount them into both leader and agents and set:
#   leader: BPM_GRPC_TLS_CERT=/run/secrets/leader.crt
#           BPM_GRPC_TLS_KEY=/run/secrets/leader.key
#           BPM_GRPC_TLS_CLIENT_CA=/run/secrets/ca.crt
#   agent:  BPM_GRPC_TLS_CLIENT_CA=/run/secrets/ca.crt
#           BPM_GRPC_TLS_CERT=/run/secrets/agent.crt
#           BPM_GRPC_TLS_KEY=/run/secrets/agent.key

set -euo pipefail

OUT="${1:-mtls-out}"
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
gen_cert agent "labextend-agent" "DNS:labextend-agent"

echo
echo "[mtls] done. Output in: $OUT"
echo "       ca.crt, leader.crt+key, agent.crt+key"
echo
echo "Create Docker secrets:"
echo "  docker secret create labextend-ca-crt     $OUT/ca.crt"
echo "  docker secret create labextend-leader-crt $OUT/leader.crt"
echo "  docker secret create labextend-leader-key $OUT/leader.key"
echo "  docker secret create labextend-agent-crt  $OUT/agent.crt"
echo "  docker secret create labextend-agent-key  $OUT/agent.key"
