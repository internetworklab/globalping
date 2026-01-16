#!/bin/bash

./buildversion.sh

docker build \
  --push \
  --tag ghcr.io/internetworklab/globalping-web:latest .
