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

The following procedures require your node have cfssl binaries installed, if they are not installed yet, please install them by

```shell
cd cfssl  # if the cfssl directory is empty, delete the clone, and re-run git clone with --recurse-submodules flag
make
make install
```

Also make sure your node already have jq and golang (both of newer version) installed and $GOPATH/bin is in the $PATH.

Now assume that you already recursive clone our repo, and cd into the project root.

1. Pick your nickname, a valid nickname is a valid dns label, satisfies regex `[a-zA-Z-_.\d]+`, for example, `json` is a valid nickname, create a directory in `confed/`, and populate the template contents:

```shell
nickname=jason
cd confed
./new-confed.sh $nickname
```

Now that you have your custom CA cert pairs in `confed/$nickname/ca.pem` and `confed/$nickname/ca-key.pem`, but don't worry, neither ca-key.pem or ca.csr will be submitted to the repo.

2. Create cert pair for your agent:

Cd into your confed directory, populate the peer's cert manifest, and invoke the gen-cert-pair.sh script to obtain a new pair of certs:

```shell
cd confed/$nickname

# set the node name for your agent, it must be a valid dns label
nodename=node1

# populate the cert manifest template
jq -n -f ./manifests/peer.json.template \
  --arg cname $nickname.$nodename \
  --argjson hosts "[\"$nodename.yourdomain.com\"]" \
  > ./manifests/$nodename.json

# generate the cert pair
./gen-cert-pair.sh ./manifests/$nodename.json
```

Now you should have your agent's cert pair as `$nodename.pem` and `$nodename-key.pem` in your confed directory.

3. Launch the globalping agent binary with carefully choosen parameters:

Obtain your IPinfo lite API token first, please access https://ipinfo.io/dashboard/lite , then write down your IPinfo lite API token as

```shell
echo "IPINFO_TOKEN=<your-ipinfo-lite-api-token>" >> .env
```

Build and start globalping binary, and serving as an agent:

```shell
go build -o bin/globalping ./cmd/globalping
nodename=<your-node-name>
http_endpoint=<public_http_endpoint_that_can_reach_your_globalping_agent> # for example: https://yournode.yourdomaon.com:18081, would be effected by --tls-listen-address parameter as well

bin/globalping agent \
  --server-address=wss://globalping-hub.exploro.one:8080/ws \
  --node-name=$nodename \
  --http-endpoint=${HTTP_ENDPOINT} \
  --peer-c-as=confed/hub/ca.pem \
  --server-name=globalping-hub.exploro.one \
  --client-cert=confed/$nickname/$nodename.pem \
  --client-cert-key=confed/$nickname/$nodename-key.pem \
  --server-cert=confed/$nickname/$nodename.pem \
  --server-cert-key=confed/$nickname/$nodename-key.pem \
  --tls-listen-address=:18081
```
