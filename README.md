# My GlobalPing

Well, this is my own toy globalping, and it has nothing to do with the more famous and more official [globalping.io](https://globalping.io), I write this code for personal interests and for fun, and I am just happy to experiment with networking technologies like sending and receiving raw IP packets, or deploying a global distributed system and securing it with mTLS.

## So, What is My GlobalPing then ?

Just as My Traceroute (MTR) combines ping and traceroute feature, and provides a CLI user interface for continuously displaying network conditions refreshed in realtime, so does My GlobalPing. My GlobalPing also combines ping and traceroute, and provides a web-based UI for continously displaying network path tracing refreshed in realtime.

What's more, My GlobalPing effortlessly supports both the Internet (we dn42 people also call it the 'Clearnet') and DN42, so you can have a clear picture about how packets traverse the BGP forwarding path, it's your good learning to help you understand the networking and routing stuffs better.

You can quickly get hands on it to see how it goes, by visit my own deploy instance of My GlobalPing at the right top of the page, or just click [here](https://globalping.netneighbor.me).

## Features

- Web-based UI for displaying realtime-refreshing Ping or Traceroute results, with multiple origin nodes and multiple targets on it simultaneously.
- Rough rDNS and IPInfoLite-like result for both Clearnet and DN42, such as country, ASN and AS Name.
- API-first design, Restful API endpoints and plain-text JSON line stream outputs, components of our system can be debugging with trivial HTTP clients such curl.

## API Design

The agents respond HTTP requests that has a path prefixed as `/simpleping` , the hub respond HTTP requests that has a path prefixed as `/ping`, both HTTP request method is GET, port number are determined by commandline argument, parameters are encoded as URL search params.

Refer to [pkg/pinger/request.go](pkg/pinger/request.go) for what parameters are supported, refer to [pkg/pinger/ping.go](pkg/pinger/ping.go) for the effects of the parameters.

Both `/simpleping` and `/ping` returns a stream of JSON lines, so the line feed character can be use as the delimiter.

When sending request to the hub, targets are encoded in `--url-query targets=` and separated by comma, when sending request to the agent, only one target supported at a time, and should be encoded in `--url-query destination`. Where `--url-query` is a syntax sugar provided by curl for handily encoding URL search params.

Also a pair of client cert is required for calling agent's API endpoint, which is protected and every requests send to it are authenticating via the mechanism of mTLS. Just refers to `bin/globalping agent --help` or `bin/globalping hub --help` for how to configure the certs.

And the APIs of the systems are not intended for being called directly from end users, only developers should do that.

## Clustering

My GlobalPing system is designed to be distributed in mind, there is a hub and a lot of agents, hub and agents are communicate through mTLS protected channels, though an agent don't talk to other agents but only talk to the hub, and hub only talk to agents and there is only one hub in a cluster.

Take a look at [docs/how-to-join.md](docs/how-to-join.md) for how to join an new agent to a cluster, actually it no more complicated than just advertising itself to the hub.
