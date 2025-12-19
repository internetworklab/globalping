# How to Generate Self-signed CA and cert pairs for m-TLS ?

We have some example cfssl JSON files in `certs/manifests`, you can generate example
self-signed CA cert pair and peer cert pairs with commands:

```shell
cd certs
./gen-ca.pem
./gen-cert-pair.sh manifests/hub.json
./gen-cert-pair.sh manifests/agent1.json
```

If you haven't installed cfssl executables, install them first:

```shell
cd cfssl
make
make install
```

# How to Prepare the Development Environment ?

1. Edit the hosts file (/etc/hosts) to make agent1.example.one and hub.example.com both points to 127.0.0.1 and ::1.
2. Generate self-signed Root CA, server cert pairs, and client cert pairs as stated above.
3. Invoke handy scripts in scripts/ folder, to launch hub, then the agent.
4. Hub listens :8080 for accepting agents registration request, :8082 for serving public un-authenticated requests.
5. Agent listens :8081 for serving requests, and the transport is an m-TLS protected, however the cert pairs generated above can both use at server-auth and client-auth, so it's fine.

# How to Deploy This System Globally ?

Mainly three steps are involved:

1. Generate a Root CA cert pair, the server's cert pair (for the hub), then generating peer cert pairs for each agent. Distribute the certs and keys to every nodes where is going to install globalping agent.
2. Build multi-arch docker image, this is the easiest way for distributing projects to machines of various archs, launch the hub first, then launch agents on the nodes.
3. We have an ansible playbook in the examples/ folder for refering, my instance of globalping is intended to be working at both the Internet and DN42, so I intentionally override docker container's default subnet (I use 192.168/16 as the replacement) so that they won't conflict with that of DN42's subnet.
4. But what about the IPv6's connectivity? I reused my dn42 ipv6 allocation and routes for the agent containers and the hub container, for dn42's IPv6 connectivity, it routes fd00::/8 to another router container in the same host (which I configure this out-of-band), and clearnet's connectivity is expressed as a fallback route (default route), ::/0 via the host netns (connected by a pair of veth) since clearnet's IPv6 almost never use fd00::/8.
