#! /usr/bin/env bash

BASE_DIR=`realpath $(dirname $0)`
cd $BASE_DIR
. util.sh

set -x

RUN=${1:-"test"}

# normal size cluster
kubeadm_up # debug
./scale_pods.sh $RUN
sleep 60
./scale_funcs.sh $RUN
sleep 60
kubeadm_down

# large cluster
kubeadm_up large # debug
./scale_nodes.sh $RUN
sleep 60
kubeadm_down
