#! /usr/bin/env bash

BASE_DIR=`realpath $(dirname $0)`
ROOT_DIR=$BASE_DIR/..
. $BASE_DIR/util.sh

set -ex

CLUSTER_NAME="dev"
# kubectl config view --minify -o jsonpath='{.clusters[0].name}'

# usage: kind.sh run [large] [debug] #workers
function run_kind {
    if [ "$1" == "large" ]; then
        large=".large"
        shift
    fi
    if [ "$1" == "debug" ]; then
        debug=".debug"
        shift
    fi
    n_workers=${1:-"1"}

    KIND_CONFIG=kind$large$debug.yaml

    mkdir -p $LOG_DIR/kind
    sudo rm -rf $LOG_DIR/kind/*
    rm -rf $MANIFESTS_DIR/kind/_tmp_*

    # pull and get hash of kind image
    kind_image=$MY_REPO/kind-node:$K8S_BUILD_TAG
    if ! docker_images | grep -qx $kind_image; then
        echo pulling $kind_image
        docker pull $kind_image
    fi
    kind_image_sha=$(docker inspect --format='{{index .RepoDigests 0}}' $kind_image)

    # populate worker nodes
    cp $MANIFESTS_DIR/kind/$KIND_CONFIG $MANIFESTS_DIR/kind/_tmp_.$KIND_CONFIG
    for ((i = 1; i <= n_workers; i++)); do
        tee -a $MANIFESTS_DIR/kind/_tmp_.$KIND_CONFIG <<EOF >/dev/null
- role: worker
  image: $kind_image_sha
EOF
    done

    kind create cluster --name $CLUSTER_NAME --config $MANIFESTS_DIR/kind/_tmp_.$KIND_CONFIG
    kubectl cluster-info --context kind-$CLUSTER_NAME

    # install metrics-server
    if [ -z "$large" ]; then
        kubectl apply -f https://github.com/kubernetes-sigs/metrics-server/releases/download/v0.7.1/components.yaml
        kubectl patch -n kube-system deployment metrics-server \
            --type='json' \
            -p='[{"op": "replace", "path": "/spec/template/spec/containers/0/args/2", "value": "--kubelet-preferred-address-types=InternalIP"},
                {"op": "add", "path": "/spec/template/spec/containers/0/args/-", "value": "--kubelet-insecure-tls"}]'
    fi
}

function watch_control_plane {
    target="kind"
    master_node=${1:-"$CLUSTER_NAME-control-plane"}

    WATCH_DIR=$ROOT_DIR/watch/$target
    WATCH_LOG=$LOG_DIR/$target
    mkdir -p $WATCH_DIR && mkdir -p $WATCH_LOG

    # kube-controller-manager
    nohup kubectl logs -n kube-system kube-controller-manager-$master_node --follow >$WATCH_LOG/controller.log 2>&1 &
    pid=$!
    echo "$pid: controller-manager -> $WATCH_LOG/controller.log"
    echo $pid >> $WATCH_DIR/pids
    
    # kube-scheduler
    nohup kubectl logs -n kube-system kube-scheduler-$master_node --follow >$WATCH_LOG/scheduler.log 2>&1 &
    pid=$!
    echo "$!: scheduler -> $WATCH_LOG/scheduler.log"
    echo $pid >> $WATCH_DIR/pids
}

function watch_kubelet {
    target="kind"
    WATCH_DIR=$ROOT_DIR/watch/$target
    WATCH_LOG=$LOG_DIR/$target/kubelet
    mkdir -p $WATCH_DIR && mkdir -p $WATCH_LOG

    nohup docker exec $1 journalctl -u kubelet --follow >$WATCH_LOG/kubelet-$1.log 2>&1 &
    pid=$!
    echo "$pid: $1 kubelet -> $WATCH_LOG/kubelet-$1.log"
    echo $pid >> $WATCH_DIR/pids
}

function clean_watch {
    target="kind"
    WATCH_DIR=$ROOT_DIR/watch/$target
    if [ -d "$WATCH_DIR" ]; then
        for pid in $(cat $WATCH_DIR/pids); do
            kill $pid || true
        done
    fi
    rm -rf $WATCH_DIR
}

function clean_kind {
    clean_watch
    kind delete cluster --name $CLUSTER_NAME
    rm -rf ~/.kube/config
    docker container prune -f
    docker network prune -f
    # docker volume prune -f
}

case "$1" in
run)
    # run [large] [debug] #workers
    shift
    run_kind $@
    ;;
watch)
    # watch [ctrl|kubelet] [ctrl:master_name|kubelet:#worker_container]
    shift
    if [ "$1" == "ctrl" ]; then
        shift
        watch_control_plane $@
    elif [ "$1" == "kubelet" ]; then
        shift
        watch_kubelet $@
    elif [ "$1" == "clean" ]; then
        clean_watch
    fi
    ;;
clean)
    clean_kind
    ;;
test)
    run_kind debug 1
    sleep 5
    watch_control_plane
    watch_kubelet $CLUSTER_NAME-worker
    ;;
esac