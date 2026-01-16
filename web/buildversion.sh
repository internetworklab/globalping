#!/bin/bash

script_path=$(realpath $0)
script_dir=$(dirname $script_path)
cd $script_dir

git rev-parse HEAD | xargs echo HEAD: >.version.txt
git --no-pager tag --points-at HEAD | xargs echo tags: >>.version.txt
git rev-parse --abbrev-ref HEAD | xargs echo branch: >> .version.txt
date --iso-8601=seconds --utc | xargs echo buildDate: >> .version.txt

cat .version.txt
