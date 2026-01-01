# How PMTU Works

![network topology](pmtu-illustration.png)

Create the above lab environment using following commands, we use ns1, ns2, and ns3 for demonstrating purpose and it will delete them and re-create them, proceed with cautious:

## Build the Lab

```shell
# Delete namespaces if they already exist
ip netns del ns1 2>/dev/null
ip netns del ns2 2>/dev/null
ip netns del ns3 2>/dev/null
# Remove the interface belonging to the host netns if it exists
ip link del v-ns1 2>/dev/null

ip netns add ns1
ip netns add ns2
ip netns add ns3
# Note: The "host netns" is your current default networking stack.

for ns in ns1 ns2 ns3; do ip -n $ns l set lo up; done

ip l add v-ns1 type veth peer eth1 netns ns1
ip l add v-ns2 netns ns1 type veth peer eth1 netns ns2
ip l add v-ns3 netns ns2 type veth peer eth1 netns ns3

ip l set v-ns1 up
ip -n ns1 l set v-ns2 up
ip -n ns1 l set eth1 up
ip -n ns2 l set v-ns3 up
ip -n ns2 l set eth1 up
ip -n ns3 l set eth1 up

ip a add 192.168.5.1/30 dev v-ns1
ip -n ns1 a add 192.168.5.2/30 dev eth1
ip -n ns1 a add 192.168.6.1/30 dev v-ns2
ip -n ns2 a add 192.168.6.2/30 dev eth1
ip -n ns2 a add 192.168.7.1/30 dev v-ns3
ip -n ns3 a add 192.168.7.2/30 dev eth1

ip -n ns1 l set v-ns2 mtu 1370
ip -n ns2 l set eth1 mtu 1370
ip -n ns2 l set v-ns3 mtu 1350
ip -n ns3 l set eth1 mtu 1350

ip -n ns3 r add 0.0.0.0/0 via 192.168.7.1
ip -n ns2 r add 0.0.0.0/0 via 192.168.6.1
ip -n ns1 r add 0.0.0.0/0 via 192.168.5.1
ip -n ns1 r add 192.168.7.0/30 via 192.168.6.2
ip r add 192.168.6.0/30 via 192.168.5.2
ip r add 192.168.7.0/30 via 192.168.5.2

sysctl -w net.ipv4.conf.v-ns1.forwarding=1
ip netns exec ns1 sysctl -w net.ipv4.conf.eth1.forwarding=1
ip netns exec ns1 sysctl -w net.ipv4.conf.v-ns2.forwarding=1
ip netns exec ns2 sysctl -w net.ipv4.conf.eth1.forwarding=1
ip netns exec ns2 sysctl -w net.ipv4.conf.v-ns3.forwarding=1

ping -i 0.2 -c2 192.168.5.1
ping -i 0.2 -c2 192.168.5.2
ping -i 0.2 -c2 192.168.6.1
ping -i 0.2 -c2 192.168.6.2
ping -i 0.2 -c2 192.168.7.1
ping -i 0.2 -c2 192.168.7.2
```

## Probing Path MTU

### Probing Path MTU With Manual Approach

To start probing MTU, let's say the MTU for the nexthop interface is 1500, so, start with our probing mtu be 1500:

```shell
ping -c1 -s $((1500-8-20)) -4 -M do 192.168.7.2
```

We got this response:

```
PING 192.168.7.2 (192.168.7.2) 1472(1500) bytes of data.
From 192.168.5.2 icmp_seq=1 Frag needed and DF set (mtu = 1370)

--- 192.168.7.2 ping statistics ---
1 packets transmitted, 0 received, +1 errors, 100% packet loss, time 0ms
```

So, adjust the probing mtu to 1370 and go ahead:

```shell
ping -c1 -s $((1370-8-20)) -4 -M do 192.168.7.2
```

Then we got this response:

```
PING 192.168.7.2 (192.168.7.2) 1342(1370) bytes of data.
From 192.168.6.2 icmp_seq=1 Frag needed and DF set (mtu = 1350)

--- 192.168.7.2 ping statistics ---
1 packets transmitted, 0 received, +1 errors, 100% packet loss, time 0ms
```

Adjust the probing mtu to 1350 and go ahead:

```shell
ping -c1 -s $((1350-8-20)) -4 -M do 192.168.7.2
```

Finally we reach the target:

```
PING 192.168.7.2 (192.168.7.2) 1322(1350) bytes of data.
1330 bytes from 192.168.7.2: icmp_seq=1 ttl=62 time=0.171 ms

--- 192.168.7.2 ping statistics ---
1 packets transmitted, 1 received, 0% packet loss, time 0ms
rtt min/avg/max/mdev = 0.171/0.171/0.171/0.000 ms
```

### Probing With Automatic Approach

With command:

```shell
traceroute --mtu 192.168.7.2
```

You get this output:

```
traceroute to 192.168.7.2 (192.168.7.2), 30 hops max, 65000 byte packets
 1  192.168.5.2 (192.168.5.2)  0.088 ms F=1500  0.074 ms  0.015 ms
 2  192.168.6.2 (192.168.6.2)  0.030 ms F=1370  0.093 ms  0.026 ms
 3  192.168.7.2 (192.168.7.2)  0.030 ms F=1350  0.101 ms  0.051 ms
```

### Kernel's PMTU Cache

Once the Path MTU is probed, the kernel might cache the PMTU result for you, you can then adjust the mtu of the nexthop interface(s) accordingly:

```
ip r get 192.168.7.2
192.168.7.2 via 192.168.5.2 dev v-ns1 src 192.168.5.1 uid 0
    cache expires 381sec mtu 1350
```

## Clean Up

```shell
ip netns del ns1
ip netns del ns2
ip netns del ns3
```
