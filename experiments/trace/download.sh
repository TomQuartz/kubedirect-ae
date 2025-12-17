#! /usr/bin/env bash

BASE_DIR=`realpath $(dirname $0)`
cd $BASE_DIR
. util.sh
lock

rm -rf dirigent && git clone https://github.com/eth-easl/dirigent.git
cd dirigent
git checkout 9715b85 >/dev/null 2>&1
git lfs pull
cd ..

mkdir -p data && rm -rf data/*
cp -r dirigent/artifact_evaluation/azure_500/dirigent/azure_500 data/

rm -rf dirigent