#! /usr/bin/env bash

BASE_DIR=`realpath $(dirname $0)`
cd $BASE_DIR

set -x

USAGE="run.sh k8s|kd #replicasets [#pods]"

export WORKLOAD=${WORKLOAD:-"test-replicaset"}
# export IMAGE=${IMAGE:-"gcr.io/google-samples/kubernetes-bootcamp:v1"}

baseline=$1
case $baseline in
    k8s)
        ;;
    kd)
        export MANAGED="true"
        ;;
    *)
        echo "Usage: $USAGE"
        exit 1
        ;;
esac
shift

n_replicasets=$1
if ! [[ -n "$1" && "$1" =~ ^[0-9]*$ ]]; then
    echo "Usage: $USAGE"
    exit 1
fi
shift

n_pods=${1:-"0"}
if ! [[ "$n_pods" =~ ^[0-9]*$ ]]; then
    echo "Usage: $USAGE"
    exit 1
fi

echo "Running replicaset breakdown experiment: baseline=$baseline, selector=$WORKLOAD, #replicasets=$n_replicasets, #pods=$n_pods"

for ((i = 0; i < n_replicasets; i++)); do
    export NAME="$WORKLOAD-$i"
    cat config/replicaset.template.yaml | envsubst | kubectl apply -f -
done

read -p "Press enter to continue..."
# sleep 60

go run . -baseline $baseline -selector $WORKLOAD -n $n_pods >result.log 2>stderr.log

# cleanup
sleep 30
kubectl delete replicaset -l workload=$WORKLOAD
