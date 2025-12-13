#! /usr/bin/env bash

BASE_DIR=`realpath $(dirname $0)`
cd $BASE_DIR
. util.sh
lock

set -x

RUN=${1:-"test"}

# normal size cluster
kubeadm_up # debug
# kubeadm_up && $ROOT_DIR/scripts/kubeadm.sh watch ctrl && $ROOT_DIR/scripts/kubeadm.sh watch kubelet
time ./scale_pods.sh $RUN
sleep 60
time ./scale_funcs.sh $RUN
sleep 60
kubeadm_down

# large cluster
kubeadm_up large # debug
# kubeadm_up large && $ROOT_DIR/scripts/kubeadm.sh watch ctrl
time ./scale_nodes.sh $RUN
sleep 60
kubeadm_down
