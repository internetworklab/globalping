#!/bin/bash

script_path=$(realpath $0)
script_dir=$(dirname $script_path)

cd $script_dir/..

go build -o bin/globalping ./cmd/globalping
bin/globalping agent \
  --node-name=agent1 \
  --http-endpoint=https://agent1.example.com:8081 \
  --peer-c-as=certs/ca.pem \
  --server-name=hub.example.com \
  --client-cert=certs/agent1.pem \
  --client-cert-key=certs/agent1-key.pem \
  --server-cert=certs/agent1.pem \
  --server-cert-key=certs/agent1-key.pem \
  --tls-listen-address=:8081 \
  --shared-quota=10
