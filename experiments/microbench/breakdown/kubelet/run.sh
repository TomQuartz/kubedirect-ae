#! /usr/bin/env bash

BASE_DIR=`realpath $(dirname $0)`
cd $BASE_DIR

set -x

USAGE="run.sh kubelet|custom #pods"

export WORKLOAD=${WORKLOAD:-"test-kubelet"}
# export IMAGE=${IMAGE:-"gcr.io/google-samples/kubernetes-bootcamp:v1"}

baseline=$1
case $baseline in
    kubelet)
        ;;
    custom)
        # NOTE: caller should setup custom kubelet service with --simulate flag
        # NOTE: if using kwok, should also setup kwok node delegation
        export LIFECYCLE="custom"
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

# choose an arbitrary worker node
for n in $(grep -v "localhost" /etc/hosts | awk '{print $NF}' | grep -vw $(hostname)); do
    if [ ! -e "$HOME/.ssh/exclude" ]; then
        node=$n
        break
    elif ! grep -Fxq $n $HOME/.ssh/exclude; then
        node=$n
        break
    fi
done
if [ -z "$node" ]; then
    echo "No available worker node found"
    exit 1
fi

echo "Running kubelet breakdown experiment: baseline=$baseline, target=$WORKLOAD, node=$node, #pods=$n_pods"

export NAME=$WORKLOAD
cat config/template-pod.yaml | envsubst | kubectl apply -f -

# create daemonset
cat config/daemonset.yaml | envsubst | kubectl apply -f -

# read -p "Press enter to continue..."
sleep 30

go run . -baseline $baseline -target $WORKLOAD -node $node -n $n_pods >result.log 2>stderr.log

# cleanup
# read -p "Press enter to continue..."
sleep 30
# cat config/template-pod.yaml | envsubst | kubectl delete -f -
kubectl delete pods -l kubedirect/owner-name=$WORKLOAD
cat config/daemonset.yaml | envsubst | kubectl delete -f -
