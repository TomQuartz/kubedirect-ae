#! /usr/bin/env bash

BASE_DIR=`realpath $(dirname $0)`
ROOT_DIR=$BASE_DIR/..
. $BASE_DIR/util.sh

set -x

KUBELET_ADDR="kubedirect/kubelet-service-addr"

# build and distribute the custom kubelet binary
function build_kubelet {
    echo "Building the custom kubelet binary..."
    cd $ROOT_DIR/cmd/kubelet
    mkdir -p bin
    go build -o bin/kubelet .

    echo "Distributing the custom kubelet binary..."
    for worker in $(workers); do
        scp bin/kubelet $worker:~/.kubedirect
    done
}

# usage: kubelet.sh run [watch] [#workers] -- args...
function run_kubelet {
    build_kubelet

    if [ "$1" == "watch" ]; then
        watch=1
        shift
    fi
    if [[ "$1" =~ ^[0-9]*$ ]] ; then
        n_workers=$1
        shift
    fi
    # parse args
    case "$1" in
    --)
        shift
        ;;
    "")
        ;;
    *)
        echo "Usage: kubelet.sh run [watch] [#workers] -- args..."
        exit 1
        ;;
    esac

    target="kubelet"
    WATCH_DIR=$ROOT_DIR/watch/$target
    WATCH_LOG=$LOG_DIR/$target
    mkdir -p $WATCH_DIR && mkdir -p $WATCH_LOG
    sudo rm -rf $WATCH_LOG/*

    for worker in $(workers $n_workers); do
        if [ -n "$watch" ]; then
            nohup ssh $worker "~/.kubedirect/kubelet $@" >$WATCH_LOG/kubelet-$worker.log 2>&1 &
        else
            nohup ssh $worker "~/.kubedirect/kubelet $@ >~/.kubedirect/kubelet-$worker.log 2>&1" >/dev/null 2>&1 &
        fi
        pid=$!
        echo "$pid" >> $WATCH_DIR/pids
    done
}

# usage: kubelet.sh delegate $node_selector [#workers]
function delegate_kubelet_service {
    selector=$1
    n_targets=$2 # empty means all
    targets=$(workers $n_targets)
    n_targets=${#targets[@]}

    echo "Delegating kubelet service of nodes matching $selector to $n_targets custom kubelets..."
    nodes=$(kubectl get nodes -l $selector -o jsonpath='{.items[*].metadata.name}')
    
    target_addrs=()
    for target in $targets; do
        addr=$(kubectl get node $target -o=json | jq -r --arg key $KUBELET_ADDR '.metadata.annotations[$key] // empty')
        if [ -z "$addr" ]; then
            echo "Error: $target has not published the $KUBELET_ADDR annotation, try later"
            exit 1
        fi
        target_addrs+=( $addr )
    done

    # exit if no available addrs
    if [ ${#target_addrs[@]} -eq 0 ]; then
        echo "Error: no available custom kubelet service"
        exit 1
    fi

    # assign each node to a custom kubelet in a round-robin fashion
    i=0
    for node in $nodes; do
        addr=${target_addrs[$i]}
        kubectl annotate node $node $KUBELET_ADDR=$addr --overwrite
        i=$(( (i+1) % ${#target_addrs[@]} ))
    done
}

function clean_kubelet {
    echo "Removing the $KUBELET_ADDR annotation from all nodes..."
    kubectl annotate nodes --all $KUBELET_ADDR-

    target="kubelet"
    WATCH_DIR=$ROOT_DIR/watch/$target
    if [ -d "$WATCH_DIR" ]; then
        for pid in $(cat $WATCH_DIR/pids); do
            kill $pid || true
        done
        n_workers=$(cat $WATCH_DIR/pids | wc -l)
        for worker in $(workers $n_workers); do
            ssh $worker "pkill -f ~/.kubedirect/kubelet"
        done
    fi
    rm -rf $WATCH_DIR
}

case "$1" in
run)
    # run [watch] [#workers] -- args...
    shift
    run_kubelet $@
    ;;
clean)
    clean_kubelet
    ;;
delegate)
    # delegate $node_selector [#workers]
    shift
    delegate_kubelet_service $@
    ;;
test)
    shift
    ;;
esac