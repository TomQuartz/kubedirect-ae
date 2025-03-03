BASE_DIR=$PWD
ROOT_DIR=$BASE_DIR/../..

# usage: kubeadm_up [large]
function kubeadm_up {
    # loop until kubeadm is up
    while true; do
        $ROOT_DIR/scripts/kubeadm.sh test $1 && break
        sleep 10
        $ROOT_DIR/scripts/kubeadm.sh clean
        sleep 10
    done
    sleep 30
}

function kubeadm_down {
    $ROOT_DIR/scripts/kubeadm.sh clean
    # sleep 30
}

# usage: custom_kubelet_up [sim]
function custom_kubelet_up {
    if [ "$1" == "sim" ]; then
        local sim="-simulate"
    fi
    $ROOT_DIR/scripts/kubelet.sh run watch -- $sim -ready-after=200 -v=2
    # sleep 30
}

function custom_kubelet_down {
    $ROOT_DIR/scripts/kubelet.sh clean
    # sleep 30
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
    # sleep 30
}

function kwok_node_down {
    kubectl delete node -l type=kwok
    # sleep 30
}