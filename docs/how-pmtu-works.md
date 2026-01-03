# How PMTU Works

MTU is the maximum size of PDU of layer 3 packets (mainly IP packets) that an interface can still happily wave through. Most ubiquitously, an Ethernet interface has a standard MTU of 1500 (bytes), meaning that it allows an IP packet of size 1500 be pass through while an IP packet with size exceeded 1500 (say, 1550) does not.

Most of the time MTU shall not trouble us, because standards, autoconfigurations, iptables stuff and firmware of the ISP residential gateway did some works for us underneath.

However, things quickly become different when tunnels (or extra layer of encapsulation) are introduced, especially when playing with virtual links or custom routings. A Tunnel outgoing interface prepends some encapsulation (like tags, labels, headers in their own) in front of the original packet to conceal the address header fields to create a virutalized and free addressing space (so that we can play with some custom routing stuffs), the encapsulation itself also cost some overheads, so the simple rule of 1500 MTU no longer applies.

Working out (also knows how to working out) the correct path MTU is important, because an interface configured with incorrect MTU might silently drops packets, causing important messages be lost. MTU mismatch or misconfiguration might also cause many BGP issues, such as flapping or ghost routes.

In this article we gonna mimics the scenario where some interfaces has non-standard MTU configured, and we will see how to probing out the correct MTU from scratch that can make the packet pass through all the interfaces along the path with no problem.

![network topology](pmtu-illustration.png)

Create the above lab environment using following commands, we use ns1, ns2, and ns3 for demonstrating purpose, and the script will delete them and re-create them, proceed with cautious:

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

To enable IPv6 connectivity and routing, paste following commands:

```shell
ip -n ns2 a add fd54:54:54:3::1/64 dev v-ns3
ip -n ns1 a add fd54:54:54:2::1/64 dev v-ns2
ip a add fd54:54:54:1::1/64 dev v-ns1

ip -n ns1 a add fd54:54:54:1::2/64 dev eth1
ip -n ns2 a add fd54:54:54:2::2/64 dev eth1
ip -n ns3 a add fd54:54:54:3::2/64 dev eth1

sysctl -w net.ipv6.conf.v-ns1.forwarding=1
ip netns exec ns1 sysctl -w net.ipv6.conf.all.forwarding=1
ip netns exec ns2 sysctl -w net.ipv6.conf.all.forwarding=1


ip -6 -n ns3 r add ::/0 via fd54:54:54:3::1
ip -6 -n ns2 r add ::/0 via fd54:54:54:2::1
ip -6 -n ns1 r add ::/0 via fd54:54:54:1::1
ip -6 -n ns1 r add fd54:54:54:3::/64 via fd54:54:54:2::2
ip -6 r add fd54:54:54:2::/64 via fd54:54:54:1::2
ip -6 r add fd54:54:54:3::/64 via fd54:54:54:1::2

ping -i 0.2 -c2 fd54:54:54:1::1
ping -i 0.2 -c2 fd54:54:54:1::2
ping -i 0.2 -c2 fd54:54:54:2::1
ping -i 0.2 -c2 fd54:54:54:2::2
ping -i 0.2 -c2 fd54:54:54:3::1
ping -i 0.2 -c2 fd54:54:54:3::2
```

## Probing Path MTU

### Probing Path MTU With Manual Approach

To start probing MTU, let's say the MTU for the nexthop interface is 1500, so, start with our probing mtu be 1500:

```shell
ping -c1 -s $((1500-8-20)) -4 -M do 192.168.7.2
# `-s <size>` specify the size of payload of ICMP echo request message:
# where 8 bytes for the ICMP header, and 20 bytes for IPv4 header,
# so `-s $((1500-8-20))` makes the size of IP PDU be exactly our current probing MTU, 1500.
# Also the probing MTU should start with the MTU of the outgoing interface to the nexthop,
# for example, if we instead probing another target 172.23.1.2, and the nexthop is connected by utun1
# and MTU of utun1 is 1420, then we should start with probing MTU of 1420.
# `-M do` make sure the router middleboxes never try to silently fragment our packets along the way.
# and in ipv6, routers just won't fragment packets.
```

We got this response:

```
PING 192.168.7.2 (192.168.7.2) 1472(1500) bytes of data.
From 192.168.5.2 icmp_seq=1 Frag needed and DF set (mtu = 1370)

--- 192.168.7.2 ping statistics ---
1 packets transmitted, 0 received, +1 errors, 100% packet loss, time 0ms
```

We can see that, the packet didn't make itself to the target, it made itself to the nexthop, 192.168.5.2, and the nexthop said the packet too big and the MTU is 1370, shrink your packet into smaller and retry again.

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

So we can see that the packet still did not make itself to the target, the second middlebox router in the path, 192.168.6.2, said the packet is still too big to pass it to the nexthop.

Similarly, we just adjust the probing mtu to 1350 and go ahead:

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
## or tracepath -n 192.168.7.2  # depending on what is installed
```

You get this output:

```
traceroute to 192.168.7.2 (192.168.7.2), 30 hops max, 65000 byte packets
 1  192.168.5.2 (192.168.5.2)  0.088 ms F=1500  0.074 ms  0.015 ms
 2  192.168.6.2 (192.168.6.2)  0.030 ms F=1370  0.093 ms  0.026 ms
 3  192.168.7.2 (192.168.7.2)  0.030 ms F=1350  0.101 ms  0.051 ms
```

### Kernel's PMTU Cache

Once the Path MTU is probed, the kernel might cache the PMTU result for you, so that your node don't have to repeat the process described above again and again when it tries to communicate with some target that has smaller interface MTU than its:

```
ip r get 192.168.7.2
192.168.7.2 via 192.168.5.2 dev v-ns1 src 192.168.5.1 uid 0
    cache expires 381sec mtu 1350
```

Next time, say one of your application tries to make a syscall `sendmsg()` to send something to, say, in our example, 192.168.7.2. Well, the kernel will make use of this finding of path MTU as discovered before, said the total length of the packet what your app gonna send is 1500 (bytes) or whatever, the kernel finds that 1500 is already greater than the PMTU cache of 192.168.7.2 which is 1350, so, depends on actual settings (e.g. some per socket options), the kernel might fragment the packets for you before actually send them out, or the kernel may also drop the packet and returns an EMSGSIZE error to the application.

You may adjust the MTU of the nexthop interface(s) accordingly, although mostly you don't have to. Note that, once you decided to adjust the MTU setting of some interface, **make sure all interface MTUs in the point-to-point link (or in the same LAN) are strictly aligned**, in our example, if you changed the MTU of v-ns1 to 1350, then you also have to change the MTU of eth1 in ns2 to 1350 so that they align.

## When PMTU Doesn't Work

There are many events that can cause normal PMTU discovery be failed, notably, some misconfigured firwalls, routers or gateways might drop ICMP messages for reasons we do or do not know, hence breaks PMTU discovery.

Beside that, in order to make the entire network functioning properly, for each link, **the MTU must match on both ends**, otherwise, normal PMTU probing might not works as expected. Look at the following topology diagram:

![mtu does not match](pmtu-doesnt-work.png)

As we saw, the MTU of v-ns3 interface in ns2 has been intentionally adjusted to 1500, which is apparently higher than the MTU of its interface that connects ns1, and most importantly, mis-aligned with the MTU of another end of the point-to-point link which is 1350.

Let's take a look at traceroute to see if it's still functioning properly:

```
traceroute --mtu 192.168.7.2
traceroute to 192.168.7.2 (192.168.7.2), 30 hops max, 65000 byte packets
 1  192.168.5.2 (192.168.5.2)  0.077 ms F=1500  0.070 ms  0.017 ms
 2  192.168.6.2 (192.168.6.2)  0.036 ms F=1370  0.142 ms  0.054 ms
 3  * * *
 4  * * *
 5  * * *
 6  * *^C
```

Well, the traceroute just stucks here. Clearly, MTU mismatch is likely causing automatic PMTU discovery to be broken.

In our case, this is because that the ns2 router thinks that, MTU 1370 is already small enough to let the packet get pass through v-ns3 (which has a MTU of 1500) to reach the nexthop, everything seems alright because 1370 < 1500, except that ns2 has no knowledge of the MTU of the peer side and never knows that 1500 MTU of v-ns3 is actually wrong (because mismatch).

To repair this, re-pair the mismatch MTU setting of v-ns3 with it's peer (eth1 in ns3, MTU 1350)

```shell
ip -n ns2 l set v-ns3 mtu 1350
traceroute --mtu 192.168.7.2
traceroute to 192.168.7.2 (192.168.7.2), 30 hops max, 65000 byte packets
 1  192.168.5.2 (192.168.5.2)  0.092 ms F=1500  0.076 ms  0.022 ms
 2  192.168.6.2 (192.168.6.2)  0.040 ms F=1370  0.109 ms  0.023 ms
 3  192.168.7.2 (192.168.7.2)  0.037 ms F=1350  0.108 ms  0.028 ms
```

Since the MTUs are now lined up, automatic PMTU discovery works again.

The way to probe the path MTU we described in this article, is sometimes also known as _the classic PMTU discovery_, and there's also a RFC for that ([rfc1191](https://datatracker.ietf.org/doc/html/rfc1191)). So now we can find that, the classic PMTU discovery does not always works, it's not always reliable, and it should be regarded as some best-effort approach. There's a much more robust way of doing PMTU discovery called _packetized PMTU_ or "Packetization Layer Path MTU Discovery" as described in [rfc4821](https://datatracker.ietf.org/doc/html/rfc4821), it's also more sophisticated and we might have a look on that in the future.

## Clean Up

```shell
ip netns del ns1
ip netns del ns2
ip netns del ns3
```
