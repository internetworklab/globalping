#!/bin/bash

script_path=$(realpath $0)
script_dir=$(dirname $script_path)
cd $script_dir

./buildversion.sh

docker buildx build \
  --platform linux/arm64,linux/amd64 \
  --push \
  --tag ghcr.io/internetworklab/globalping:latest .
