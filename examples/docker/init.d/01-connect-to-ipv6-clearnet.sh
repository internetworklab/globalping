#!/bin/bash

script_path=$(realpath $0)
script_dir=$(dirname $script_path)

source $script_dir/../.env

if [ -z "$DN42_IPV6" ]; then
    echo "Error: DN42_IPV6 is not set"
    exit 1
fi

echo "dn42 ipv6: $DN42_IPV6"

containername="globalping-agent"

pid2=$(docker inspect $containername -f {{.State.Pid}})
if [ -z "$pid2" ]; then
    echo "Error: Failed to get pid of $containername container"
    exit 1
fi

echo "$containername pid: $pid2"

ip l del v-gping 2>/dev/null || true
nsenter -t $pid2 -n ip l del v-host 2>/dev/null || true

# connects globalping-agent with host-netns (so that give it clearnet access)

ip l add v-gping type veth peer v-host netns $pid2
nsenter -t $pid2 -n ip l set v-host up
nsenter -t $pid2 -n ip a flush scope link dev v-host
nsenter -t $pid2 -n ip a add fe80::2/64 dev v-host
nsenter -t $pid2 -n ip r add ::/0 via fe80::1 dev v-host

ip l set v-gping up
ip a flush scope link dev v-gping
ip a add fe80::1/64 dev v-gping
ip route add $DN42_IPV6/128 via fe80::2 dev v-gping src 2a0a:4cc0:0:2573:48bc:85ff:fe51:e967
sysctl -w net.ipv6.conf.v-gping.forwarding=1
