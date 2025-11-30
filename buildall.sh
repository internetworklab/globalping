#!/bin/bash

docker buildx build \
  --no-cache \
  --platform linux/arm64,linux/amd64 \
  --push \
  --tag ghcr.io/internetworklab/globalping:latest .
