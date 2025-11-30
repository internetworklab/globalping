#!/bin/bash

script_path=$(realpath $0)
script_dir=$(dirname $script_path)

source $script_dir/../.env

if [ -z "$NODE_NAME" ]; then
    echo "Error: NODE_NAME is not set"
    exit 1
fi

if [ -z "$DN42_IPV4" ]; then
    echo "Error: DN42_IPV4 is not set"
    exit 1
fi

if [ -z "$DN42_IPV6" ]; then
    echo "Error: DN42_IPV6 is not set"
    exit 1
fi

echo "node: $NODE_NAME"
echo "dn42 ipv4: $DN42_IPV4"
echo "dn42 ipv6: $DN42_IPV6"


pid1=$(docker inspect bird -f {{.State.Pid}})
if [ -z "$pid1" ]; then
    echo "Error: Failed to get pid of bird container"
    exit 1
fi

echo "bird pid: $pid1"

pid2=$(docker inspect global-pinger-proxy -f {{.State.Pid}})
if [ -z "$pid2" ]; then
    echo "Error: Failed to get pid of global-pinger-proxy container"
    exit 1
fi

echo "global-pinger-proxy pid: $pid2"

nsenter -t $pid1 -n ip l del v-gping 2>/dev/null || true
nsenter -t $pid2 -n ip l del v-bird 2>/dev/null || true

ip l add v-gping netns $pid1 type veth peer v-bird netns $pid2
nsenter -t $pid1 -n ip l set v-gping master vrf42
nsenter -t $pid1 -n ip l set v-gping up
nsenter -t $pid1 -n ip a flush scope link dev v-gping
nsenter -t $pid1 -n ip a add fe80::1/64 dev v-gping
nsenter -t $pid1 -n ip r add $DN42_IPV4/32 via inet6 fe80::2 dev v-gping vrf vrf42
nsenter -t $pid1 -n ip r add $DN42_IPV6/128 via fe80::2 dev v-gping vrf vrf42

nsenter -t $pid2 -n ip l set v-bird up
nsenter -t $pid2 -n ip a add $DN42_IPV4/32 dev v-bird
nsenter -t $pid2 -n ip a add $DN42_IPV6/128 dev v-bird
nsenter -t $pid2 -n ip a flush scope link dev v-bird
nsenter -t $pid2 -n ip a add fe80::2/64 dev v-bird
nsenter -t $pid2 -n ip r add 172.20.0.0/14 via inet6 fe80::1 dev v-bird
nsenter -t $pid2 -n ip r add fd00::/8 via fe80::1 dev v-bird
