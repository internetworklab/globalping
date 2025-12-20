# Joining a New Agent to the Cluster

TL;DR: If you are in a hurry, feel free to skip the bullshits and go straight to section 3. [How to Setup And Connect](#how-to-setup-and-connect)

## Authentication is The Key

In its simpliest form, joining a new agent to the cluster can just be as simple as letting the joining agent introduce itself to the hub, and without authentication, the agent can just announce itself as whoever it wanna to be.

Imagine this, "Hey, I am agent one, and you can just call me agent one if you like", said agent1 to the hub. In an ideal world where everyone is desirably honest then this is already the perfect solution.

However, this is apparently not the case, because if every agent can be advertised as any name, chaos would occur. This is where mTLS (a way of doing mutual TLS authentication) comes in.

## How the Cluster Conceptually Works ?

A cluster is consists of a single hub and a set of agents, agents can join or leave at anytime but the hub stays.

The hub is no more than just a broker who sit betwwen the customer (the client) and the ones who actually make things happen. What makes a hub a hub is that the hub knows more, about, for example, who's there, who can do what, and how to reach the actual doers.

All because, every agent, when starting, will just proactively send its infos to the hub, including node name, node's capabilities, and node's public http endpoint by which the hub invoke the agent's services.

## How to Setup and Connect

todo.
