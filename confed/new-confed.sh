#!/bin/bash

script_path=$(realpath $0)
script_dir=$(dirname $script_path)
cd $script_dir

nickname=$1
if [ -z "$nickname" ]; then
    echo "Usage: $0 <nickname>"
    exit 1
fi

cp -r template "$nickname"
cd "$nickname"
jq -n -f ./manifests/ca.json.template --arg ca_cname "$nickname-ca" > ./manifests/ca.json
./gen-ca.sh
