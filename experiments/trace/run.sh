#! /usr/bin/env bash

BASE_DIR=`realpath $(dirname $0)`
cd $BASE_DIR

set -x

USAGE="run.sh kd|k8s+|kd+ [#traces] -- args..."
# kn args: -v=1 [-backend=grpc]
# k8s+|kd+ args: -v=1 -backend=[grpc|fake] 

tag=${TAG:-"dev"}
export IMAGE=${IMAGE:-"shengqipku/kubedirect-bench:$tag"}

# args inferred from baseline:
# - arg_gateway, arg_autoscaler, arg_autoscaler_config
# args from caller:
# - arg_backend
arg_loader="-loader-config=config/loader.json"
arg_output="-output=trace.log"

baseline=$1
case $baseline in
    "kd")
        trace_template="config/kd.ksvc.template.yaml"
        workload_daemonset="config/kd.daemonset.yaml"
        arg_gateway="-gateway=knative"
        ;;
    # NOTE: for + baselines, caller should setup custom kubelet service WITHOUT --simulate flag
    "k8s+")
        trace_template="config/k8s.deployment.template.yaml"
        workload_daemonset="config/k8s.daemonset.yaml"
        arg_gateway="-gateway=k8s"
        arg_autoscaler="-autoscaler=kpa"
        arg_autoscaler_config="-autoscaler-config=config/autoscaler.knative.yaml"
        ;;
    "kd+")
        trace_template="config/kd.deployment.template.yaml"
        workload_daemonset="config/kd.daemonset.yaml"
        arg_gateway="-gateway=k8s"
        arg_autoscaler="-autoscaler=kpa"
        arg_autoscaler_config="-autoscaler-config=config/autoscaler.dirigent.yaml"
        ;;
    *)
        echo "Usage: $USAGE"
        exit 1
        ;;
esac
shift

n_traces="500"
if [[ -n "$1" && "$1" =~ ^[0-9]*$ ]] ; then
    if [ "$1" -le 0 ] || [ "$1" -gt 500 ]; then
        echo "Invalid number of traces: $1, should be in [1, 500]"
    fi
    n_traces=$1
    shift
fi

# parse extra args
case "$1" in
--)
    shift
    ;;
"")
    ;;
*)
    echo "Usage: $USAGE"
    exit 1
    ;;
esac

echo "Running trace experiment: baseline=$baseline, #traces=$n_traces"

for ((i = 0; i < n_traces; i++)); do
    export NAME="trace-$i"
    cat $trace_template | envsubst | kubectl apply -f -
done

# create daemonset
export NAME="workload-daemonset"
cat $workload_daemonset | envsubst | kubectl apply -f -

# read -p "Press enter to continue..."
sleep 120

echo "Starting trace client with args: $@ $arg_gateway $arg_backend $arg_autoscaler $arg_autoscaler_config $arg_loader $arg_output"

go run . $@ $arg_gateway $arg_backend $arg_autoscaler $arg_autoscaler_config $arg_loader $arg_output \
    >stderr.log 2>&1

# cleanup
sleep 30
case $baseline in
"kd")
    kubectl delete ksvc --all || true
    kubectl delete cfg --all || true
    kubectl delete rev --all || true
    kubectl delete route --all || true
    kubectl delete deployment -l workload=trace || true
    kubectl delete replicaset -l workload=trace || true
    ;;
"k8s+"|"kd+")
    kubectl delete deployment -l workload=trace || true
    ;;
esac
cat $workload_daemonset | envsubst | kubectl delete -f -
