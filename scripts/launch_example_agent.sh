#!/bin/bash

script_path=$(realpath $0)
script_dir=$(dirname $script_path)

cd $script_dir/..

bin/globalping agent \
  --tls-listen-address=":8083" \
  --http-listen-address="127.0.0.1:8084" \
  --server-cert=/root/services/globalping/agent/certs/peer.pem \
  --server-cert-key=/root/services/globalping/agent/certs/peer-key.pem \
  --metrics-listen-address="127.0.0.1:2113" \
  --ip-2-location-api-endpoint="https://api.ip2location.io" 
