#! /usr/bin/env bash

BASE_DIR=`realpath $(dirname $0)`
cd $BASE_DIR
. util.sh

set -x

RUN=${1:-"test"}

# NOTE: assume k8s is up and running
# usage: scale_funcs.sh $RUN

setup_dirs scale-funcs || exit 0

N_FUNCS=(100 200 300 600)
# N_FUNCS=(40)
n_pods_per_func=1

# usage: run_cmd $name $cmd $baselines...
function run_cmd {
    name=$1
    cmd=$2
    shift 2
    for baseline in $@; do
        for n_funcs in ${N_FUNCS[@]}; do
            eval "$cmd"
            cp ./result.log $RESULTS/$name.$baseline.$n_funcs.log
            cp ./stderr.log $RESULTS/stderr/$name.$baseline.$n_funcs.log
            sleep 30
        done
    done
}

###################### e2e ######################
cd $BASE_DIR/e2e
cmd="./run.sh \$baseline \$n_funcs \$((n_funcs * n_pods_per_func))"

# use default kubelet for k8s/kd
run_cmd e2e "$cmd" k8s kd

# use custom kubelet for k8s+/kd+
custom_kubelet_up # watch
run_cmd e2e "$cmd" k8s+ kd+
custom_kubelet_down

###################### breakdown: autoscaler ######################
cd $BASE_DIR/breakdown/autoscaler
cmd="./run.sh \$baseline \$n_funcs \$((n_funcs * n_pods_per_func))"
run_cmd _as "$cmd" k8s kd

###################### breakdown: replicaset ######################
# NOTE: we include replicaset in function scalability
# because the rs controller may parallel the reconciliation of multiple replicasets
cd $BASE_DIR/breakdown/replicaset
cmd="./run.sh \$baseline \$n_funcs \$((n_funcs * n_pods_per_func))"
run_cmd _rs "$cmd" k8s kd

###################### breakdown: endpoints ######################
cd $BASE_DIR/breakdown/endpoints
cmd="./run.sh \$baseline \$n_funcs \$((n_funcs * n_pods_per_func))"
run_cmd _ep "$cmd" k8s kd
