#! /usr/bin/env bash

BASE_DIR=`realpath $(dirname $0)`
cd $BASE_DIR
. util.sh

set -x

kubeadm_up # large debug
custom_kubelet_up sim
kwok_up
kwok_node_up 10

kubectl apply -f daemonset.yaml
kubectl apply -f test-kubelet.yaml
kubectl scale deployment test --replicas=3

kubectl delete -f test-kubelet.yaml
kubectl delete -f daemonset.yaml

kwok_node_down
kwok_down
custom_kubelet_down
kubeadm_down
