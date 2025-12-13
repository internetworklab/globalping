#!/bin/bash

script_path=$(realpath $0)
script_dir=$(dirname $script_path)

cd $script_dir/..

go build -o bin/globalping ./cmd/globalping
bin/globalping hub \
  --peer-c-as=certs/ca.pem \
  --client-cert=certs/hub.pem \
  --client-cert-key=certs/hub-key.pem \
  --server-cert=certs/hub.pem \
  --server-cert-key=certs/hub-key.pem \
  --web-socket-path=/ws \
  --address :8080
