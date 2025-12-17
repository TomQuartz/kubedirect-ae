#! /usr/bin/env bash

BASE_DIR=`realpath $(dirname $0)`
cd $BASE_DIR
. util.sh

set -x

RUN=${1:-"test"}

# NOTE: assume k8s is up and running
# usage: scale_pods.sh $RUN

setup_dirs scale-pods

N_PODS=(100 200)
n_nodes=20
# N_PODS=(40)
# n_nodes=2

# usage: run_cmd $name $cmd $baselines...
function run_cmd {
    name=$1
    cmd=$2
    shift 2
    for baseline in $@; do
        for n_pods in ${N_PODS[@]}; do
            out=$name.$baseline.$n_pods
            if [ -s "$RESULTS/$out.log" ]; then
                echo "found result for $out in $RESULTS, skipping"
                continue
            fi
            eval "$cmd"
            cp ./result.log $RESULTS/$out.log
            cp ./stderr.log $RESULTS/stderr/$out.log
            sleep 30
        done
    done
}

###################### e2e ######################
cd $BASE_DIR/e2e
cmd="./run.sh \$baseline 1 \$n_pods"

# use default kubelet for k8s/kd
run_cmd e2e "$cmd" k8s kd

# use custom kubelet for k8s+/kd+
custom_kubelet_up # watch
run_cmd e2e "$cmd" k8s+ kd+
custom_kubelet_down

# ###################### breakdown: autoscaler ######################
# cd $BASE_DIR/breakdown/autoscaler
# cmd="./run.sh \$baseline 1 \$n_pods"
# run_cmd _as "$cmd" k8s kd

# ###################### breakdown: deployment ######################
# cd $BASE_DIR/breakdown/deployment
# cmd="./run.sh \$baseline 1 \$n_pods"
# run_cmd _dp "$cmd" k8s kd

###################### breakdown: replicaset ######################
cd $BASE_DIR/breakdown/replicaset
cmd="./run.sh \$baseline 1 \$n_pods"
run_cmd _rs "$cmd" k8s kd

###################### breakdown: scheduler ######################
cd $BASE_DIR/breakdown/scheduler
cmd="./run.sh \$baseline \$n_pods"
run_cmd _sched "$cmd" k8s kd

###################### breakdown: kubelet ######################
cd $BASE_DIR/breakdown/kubelet
cmd="./run.sh \$baseline \$((n_pods / n_nodes))"

# use default kubelet
run_cmd _runtime "$cmd" kubelet

# use custom kubelet
custom_kubelet_up # watch
run_cmd _runtime "$cmd" custom
custom_kubelet_down

# ###################### breakdown: endpoints ######################
# cd $BASE_DIR/breakdown/endpoints
# cmd="./run.sh \$baseline 1 \$n_pods"
# run_cmd _ep "$cmd" k8s kd

###################### generate plots ######################
cd $BASE_DIR
python3 plot.py scale-pods $RUN