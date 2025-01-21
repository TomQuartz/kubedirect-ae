#! /usr/bin/env bash

BASE_DIR=`realpath $(dirname $0)`
cd $BASE_DIR
. util.sh

set -x

RUN=${1:-"test"}
verbosity=${2:-"0"}
n_traces=${3:-"500"}

# normal size cluster
kubeadm_up # debug
# kubeadm_up debug && $ROOT_DIR/scripts/kubeadm.sh watch ctrl && $ROOT_DIR/scripts/kubeadm.sh watch kubelet

# custom data plane
custom_kubelet_up # watch
# custom_kubelet_up watch
for baseline in k8s+ kd+; do
    setup_dirs $baseline || continue
    ./run.sh $baseline $n_traces -- -backend=grpc -v=$verbosity
    cp ./trace.log $RESULTS/$baseline.$n_traces.log
    cp ./stderr.log $RESULTS/stderr/$baseline.$n_traces.log
done
custom_kubelet_down

# knative data plane
knative_up
for baseline in kd ; do
    setup_dirs $baseline || continue
    ./run.sh $baseline $n_traces -- -backend=grpc -v=$verbosity
    cp ./trace.log $RESULTS/$baseline.$n_traces.log
    cp ./stderr.log $RESULTS/stderr/$baseline.$n_traces.log
done
knative_down

kubeadm_down