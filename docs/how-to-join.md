# Joining a New Agent to the Cluster

TL;DR: If you are in a hurry, feel free to skip the bullshit and go straight to section 3. [How to Setup And Connect](#how-to-setup-and-connect)

## Authentication is The Key

In its simplest form, joining a new agent to the cluster can be as simple as letting the joining agent introduce itself to the hub, and without authentication, the agent can just announce itself as whoever it wants to be.

Imagine this, "Hey, I am agent one, and you can just call me agent one if you like", said agent1 to the hub. In an ideal world where everyone is honest, this is already the perfect solution.

However, this is apparently not the case, because if every agent can be advertised as any name, chaos would occur. This is where mTLS (a way of doing mutual TLS authentication) comes in.

## How the Cluster Conceptually Works

A cluster consists of a single hub and a set of agents; agents can join or leave at any time but the hub stays.

The hub is no more than just a broker who sits between the customer (the client) and the ones who actually make things happen. What makes a hub a hub is that the hub knows more about, for example, who's there, who can do what, and how to reach the actual doers.

This is because every agent, when starting, will proactively send its information to the hub, including node name, node's capabilities, and node's public HTTP endpoint by which the hub invokes the agent's services.

## How to Setup and Connect

Make sure golang of newer version is installed in your system:

```shell
curl -L -o - https://go.dev/dl/go1.25.5.linux-amd64.tar.gz | tar -C /usr/local -z -x -v -f -
# append /usr/local/go/bin to $PATH
```

The following procedures require your node have cfssl binaries installed, if they are not installed yet, please install them by

```shell
cd cfssl  # if the cfssl directory is empty, delete the clone, and re-run git clone with --recurse-submodules flag
make
make install
```

Also make sure your node already has jq and golang (both of newer versions) installed and $GOPATH/bin is in the $PATH.

Your node would require both Internet ('Clearnet') and DN42 connectivity.

Now assume that you have already recursively cloned our repo, and cd into the project root.

1. Pick your nickname, a valid nickname is a valid dns label, satisfies regex `[a-zA-Z-_.\d]+`, for example, `jason` is a valid nickname, create a directory in `confed/`, and populate the template contents:

```shell
nickname=jason
cd confed
./new-confed.sh $nickname
```

Now that you have your custom CA cert pairs in `confed/$nickname/ca.pem` and `confed/$nickname/ca-key.pem`, but don't worry, neither ca-key.pem or ca.csr will be submitted to the repo.

Once your CA cert is created, you can start to submit the changes to the repo [internetworklab/globalping](https://github.com/internetworklab/globalping): Create your own fork, commit and push the changes to your fork repo, open a new PullRequest and wait. You don't have to hurry to do the next steps before the PR is merged, as the hub needs to know your CA cert before it can validate your agent's requests.

After the PR is merged, start the following steps:

2. Create cert pair for your agent:

Cd into your confed directory, populate the peer's cert manifest, and invoke the gen-cert-pair.sh script to obtain a new pair of certs:

```shell
cd confed/$nickname

# set the node name for your agent, it must be a valid dns label
nodename=node1

# populate the cert manifest template
jq -n -f ./manifests/peer.json.template \
  --arg cname $nickname.$nodename \
  --argjson hosts "[\"$nodename.yourdomain.com\", \"*.yourdomain.com\" ]" \
  > ./manifests/$nodename.json

# generate the cert pair
./gen-cert-pair.sh ./manifests/$nodename.json
```

Note: the hosts list is critical, say in step3, the public https endpoint of your agent is `https://<domainx>:<port>`, then `<domainx>` must be included in the hosts list above.

For example, if the public https endpoint of your nyc1 agent is `https://nyc1.yourdomain.com:18081` (port doesn't matter), then you should make sure that, the domain `nyc1.yourdomain.com` is actually included in the generated cert, you can view this by running `openssl x509 -in path/to/cert.pem -noout -text` . And of course, the domain also have to resolved to the IP and IPv6 address of your node where the agent is deployed.

Now you should have your agent's cert pair as `$nodename.pem` and `$nodename-key.pem` in your confed directory.

You can check that, if the hub recognizes your certs, using this command:

```shell
openssl s_client \
  -CAfile confed/hub/ca.pem \  # this is the ca.pem of the hub
  -cert confed/$nickname/$nodename.pem \        # this is the cert you just created
  -key confed/$nickname/$nodename-key.pem \     # this is the cert key you just created
  -servername globalping-hub.exploro.one \       # this is the TLS server name of the hub
  -connect globalping-hub.exploro.one:8080 \     # this is the actual host:port endpoint of the hub's tls listener
  -verify_return_error   # abort on error
```

If this command was failed to run, reach out to the maintainer (i.e. me).

3. Launch the globalping agent binary with carefully chosen parameters:

Obtain your IPinfo Lite API token first, please access https://ipinfo.io/dashboard/lite, then write down your IPinfo Lite API token as

```shell
echo "IPINFO_TOKEN=<your-ipinfo-lite-api-token>" >> .env
```

Obtain your IP2Location Free-tier API Key, then

```shell
echo "IP2LOCATION_API_KEY=<your-ip2location-api-key>" >> .env
```

Build and start globalping binary, and serving as an agent:

```shell
go build -o bin/globalping ./cmd/globalping
nodename=<your-node-name>
http_endpoint=<public_http_endpoint_that_can_reach_your_globalping_agent> # for example: https://yournode.yourdomain.com:18081, would be affected by --tls-listen-address parameter as well

peer_ca=https://github.com/internetworklab/globalping/raw/refs/heads/master/confed/hub/ca.pem

EXACT_LOCATION_LAT_LON=$(curl -s https://ipinfo.io | jq -r '.loc')

bin/globalping agent \
  --server-address=wss://globalping-hub.exploro.one:8080/ws \
  --node-name=$nodename \
  --http-endpoint=${HTTP_ENDPOINT} \
  --peer-c-as=$peer_ca \
  --server-name=globalping-hub.exploro.one \
  --client-cert=confed/$nickname/$nodename.pem \
  --client-cert-key=confed/$nickname/$nodename-key.pem \
  --server-cert=confed/$nickname/$nodename.pem \
  --server-cert-key=confed/$nickname/$nodename-key.pem \
  --tls-listen-address=:18081 \
  --respond-range=172.20.0.0/14 \
  --respond-range=fd00::/8 \
  --respond-range=10.127.0.0/16 \
  --metrics-listen-address=127.0.0.1:2112 \
  --exact-location-lat-lon=${EXACT_LOCATION_LAT_LON} \
  --support-udp=true \
  --support-pmtu=true \
  --support-tcp=true \
  --ip-2-location-api-endpoint=https://api.ip2location.io \
  --dn-42-ip-2-location-api-endpoint=https://regquery.ping2.sh/ip2location/v1/query \
  --support-dns=true \
  --quic-server-address=globalping-hub.exploro.one:18443

```

By default, the agent use default public endpoint ":2112" to expose prometheus metrics, if you don't like it to be public, just change it to

```
--metrics-listen-address="127.0.0.1:2112"
```

You can curl `127.0.0.1:2112/metrics` to see what prometheus metrics it published.

Note: When choosing the node name ($nodename), we suggest that, use some format like `<your-nickname>/<location><number>` or `<your-nickname>/<country-or-region>-<location>-<number>`, for example, `jason/nyc1`, `jason/us-nyc-01` are both good node name for an agent deployed at somewhere near NYC.

To recap, mainly three steps are involved:

1. Build your custom CA and submit it to the repo;
2. Create (and maybe distribute) new cert pair for your new agent;
3. Populate .env with your IPinfo token, and launch your globalping binary using proper parameters.
