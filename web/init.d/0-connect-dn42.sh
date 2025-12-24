#!/bin/bash

my_ip=192.168.252.2
dn42_ip=172.20.143.39
dn42_ip6=fdda:8ca4:1556:a004::

pid1=$(docker inspect bird -f '{{.State.Pid}}')
if [ -z "$pid1" ]; then
    echo "Bird container not found"
    exit 1
fi

echo "pid1 (bird): $pid1"

pid2=$(docker inspect globalping-web -f '{{.State.Pid}}')
if [ -z "$pid2" ]; then
    echo "Globalping web container not found"
    exit 1
fi

echo "pid2 (globalping-web): $pid2"

ip l add v-bird netns $pid2 type veth peer name v-gpingweb netns $pid1

# point globalping-web -> bird (v4)
nsenter -t $pid2 -n ip l set v-bird up
nsenter -t $pid2 -n ip a flush scope link dev v-bird
nsenter -t $pid2 -n ip a add fe80::2/64 dev v-bird
nsenter -t $pid2 -n ip r add 172.20.0.0/14 via inet6 fe80::1 dev v-bird
nsenter -t $pid2 -n ip r add 10.127.0.0/16 via inet6 fe80::1 dev v-bird

# point bird -> globalping-web (v4)
nsenter -t $pid1 -n ip l set v-gpingweb up
nsenter -t $pid1 -n ip l set v-gpingweb vrf vrf42
nsenter -t $pid1 -n ip a flush scope link dev v-gpingweb
nsenter -t $pid1 -n ip a add fe80::1/64 dev v-gpingweb
nsenter -t $pid1 -n sysctl -w net.ipv4.conf.v-gpingweb.forwarding=1
nsenter -t $pid1 -n ip r add $my_ip/32 via inet6 fe80::2 dev v-gpingweb src $dn42_ip vrf vrf42 
nsenter -t $pid1 -n nft add table ip nat
nsenter -t $pid1 -n nft delete chain ip nat dnat-gpingweb 2>/dev/null || true
nsenter -t $pid1 -n nft add chain ip nat dnat-gpingweb { type nat hook prerouting priority dstnat ';' policy accept ';' }
nsenter -t $pid1 -n nft add rule ip nat dnat-gpingweb ip daddr $dn42_ip tcp dport { 80, 443 } dnat to $my_ip
nsenter -t $pid1 -n nft delete chain ip nat snat-gpingweb 2>/dev/null || true
nsenter -t $pid1 -n nft add chain ip nat snat-gpingweb { type nat hook postrouting priority srcnat ';' policy accept ';' }
nsenter -t $pid1 -n nft add rule ip nat snat-gpingweb ip saddr $my_ip/32 masquerade

# point bird -> globalping-web (v6)
nsenter -t $pid1 -n ip -6 r add $dn42_ip6/128 via fe80::2 dev v-gpingweb vrf vrf42

# point globalping-web -> bird (v6)
nsenter -t $pid2 -n ip a add $dn42_ip6/128 dev v-bird
nsenter -t $pid2 -n ip r add fd00::/8 via fe80::1 dev v-bird
