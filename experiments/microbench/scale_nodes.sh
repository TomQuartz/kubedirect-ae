#! /usr/bin/env bash

BASE_DIR=`realpath $(dirname $0)`
cd $BASE_DIR
. util.sh

set -x

RUN=${1:-"test"}

# NOTE: assume k8s is up and running
# NOTE: we only test the scalability of kd+ using custom kubelet and kwok nodes
# usage: scale_nodes.sh $RUN

setup_dirs scale-nodes || exit 0

N_NODES=(500 1000 1500 2000)
# N_NODES=(100)
n_pods_per_node=5

# usage: run_cmd_with_nodes #nodes $name $cmd $baselines...
function run_cmd_with_nodes {
    n_nodes=$1
    name=$2
    cmd=$3
    shift 3
    for baseline in $@; do
        eval "$cmd"
        cp ./result.log $RESULTS/$name.$baseline.$n_nodes.log
        cp ./stderr.log $RESULTS/stderr/$name.$baseline.$n_nodes.log
        sleep 120
    done
}

# assume custom kubelet and kwok nodes are up
function run_all_with_nodes {

n_nodes=$1
shift
###################### e2e ######################
cd $BASE_DIR/e2e
cmd="./run.sh \$baseline 1 \$((n_nodes * n_pods_per_node))"
# use custom kubelet for kd+
run_cmd_with_nodes $n_nodes e2e "$cmd" kd+

###################### breakdown: scheduler ######################
cd $BASE_DIR/breakdown/scheduler
cmd="./run.sh \$baseline \$((n_nodes * n_pods_per_node))"
# NOTE: must pass LIFECYCLE=custom because we are using kwok nodes
LIFECYCLE=custom run_cmd_with_nodes $n_nodes _sched "$cmd" kd

###################### breakdown: kubelet ######################
# NOTE: we include kubelet in node scalability
# because increasing the number of kubelets increases the load on the api server
cd $BASE_DIR/breakdown/kubelet
cmd="./run.sh \$baseline \$n_pods_per_node"
# use custom kubelet
run_cmd_with_nodes $n_nodes _runtime "$cmd" custom

}

custom_kubelet_up # watch
kwok_up

for n_nodes in ${N_NODES[@]}; do
    kwok_node_up $n_nodes
    run_all_with_nodes $n_nodes
    kwok_node_down
done

kwok_down
custom_kubelet_down
