BASE_DIR=`realpath $(dirname $0)`
ROOT_DIR=$BASE_DIR/../..

function lock {
    local lockfd=${1:-"2026"}
    eval "exec $lockfd>$ROOT_DIR/run.lock"
    flock -n $lockfd || {
        echo "Another instance is running. Please wait for it to finish."
        exit 1
    }
}

function setup_dirs {
    RUN=${RUN:-"test"}
    RESULTS=$BASE_DIR/results/$1/$RUN

    if [ -d "$RESULTS" ]; then
        echo "WARNING: $1/$RUN already exists"
        return 1
    else
        mkdir -p $RESULTS
        mkdir -p $RESULTS/stderr
    fi
}

# usage: kubeadm_up [large] [debug] [#workers]
function kubeadm_up {
    # loop until kubeadm is up
    while true; do
        $ROOT_DIR/scripts/kubeadm.sh run $@ && break
        sleep 10
        $ROOT_DIR/scripts/kubeadm.sh clean
        sleep 10
    done
    sleep 30
}

function kubeadm_down {
    $ROOT_DIR/scripts/kubeadm.sh clean
    sleep 60
}

# usage: custom_kubelet_up [watch] [#workers]
function custom_kubelet_up {
    if [ "$1" == "watch" ]; then
        local verbose="-v=2"
    fi
    $ROOT_DIR/scripts/kubelet.sh run $@ -- -simulate -ready-after=200 $verbose
    sleep 30
}

function custom_kubelet_down {
    $ROOT_DIR/scripts/kubelet.sh clean
    sleep 30
}

function kwok_up {
    kubectl apply -f $ROOT_DIR/manifests/kwok/kwok.yaml
    kubectl apply -f $ROOT_DIR/manifests/kwok/lifecycle.yaml
}

function kwok_down {
    kubectl delete -f $ROOT_DIR/manifests/kwok/lifecycle.yaml
    kubectl delete -f $ROOT_DIR/manifests/kwok/kwok.yaml
}

function kwok_node_up {
    n_nodes=$1
    for ((i = 0; i < n_nodes; i++)); do
        NODENAME="kwok-$i" envsubst < $ROOT_DIR/manifests/kwok/node.yaml | kubectl apply -f -
    done
    sleep 30

    $ROOT_DIR/scripts/kubelet.sh delegate type=kwok
    sleep 30
}

function kwok_node_down {
    kubectl delete node -l type=kwok
    sleep 30
}