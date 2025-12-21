#!/bin/bash

script_path=$(realpath $0)
script_dir=$(dirname $script_path)

cd $script_dir

manifestJSON=$1
if [ -z "$manifestJSON" ]; then
    echo "Usage: $0 <manifest.json>"
    exit 1
fi

base_name=$(basename $manifestJSON .json)
if [ -z "$base_name" ]; then
    echo "Failed to determine base name from $manifestJSON"
    exit 1
fi

cfssl gencert \
  -ca ca.pem \
  -ca-key ca-key.pem \
  $manifestJSON | \
cfssljson -bare $base_name

