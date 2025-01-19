#! /usr/bin/env bash

BASE_DIR=`realpath $(dirname $0)`
cd $BASE_DIR

set -x

USAGE="run.sh k8s|kd #pods"
# NOTE: if using kwok, then caller should setup custom kubelet service with --simulate flag + kwok node delegation
# NOTE: must also export LIFECYCLE=custom env var

export WORKLOAD=${WORKLOAD:-"test-scheduler"}
# export IMAGE=${IMAGE:-"gcr.io/google-samples/kubernetes-bootcamp:v1"}

baseline=$1
case $baseline in
    k8s)
        export FALLBACK="binding"
        ;;
    kd)
        ;;
    *)
        echo "Usage: $USAGE"
        exit 1
        ;;
esac
shift

n_pods=$1
if ! [[ -n "$1" && "$1" =~ ^[0-9]*$ ]]; then
    echo "Usage: $USAGE"
    exit 1
fi
shift

echo "Running scheduler breakdown experiment: baseline=$baseline, target=$WORKLOAD, #pods=$n_pods"

export NAME=$WORKLOAD
cat config/template-pod.yaml | envsubst | kubectl apply -f -

read -p "Press enter to continue..."
# sleep 60

go run . -baseline $baseline -target $WORKLOAD -n $n_pods >result.log 2>stderr.log

# cleanup
sleep 30
cat config/template-pod.yaml | envsubst | kubectl delete -f -
