BASE_DIR=`realpath $(dirname $0)`
ROOT_DIR=$BASE_DIR/../..

function setup_dirs {
    RUN=${RUN:-"test"}
    RESULTS=$BASE_DIR/results/$1/$RUN

    if [ -d "$RESULTS" ]; then
        echo "Already finished experiment $1/$RUN"
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
    $ROOT_DIR/scripts/kubelet.sh run $@ -- -ready-after=200 $verbose
    sleep 30
}

function custom_kubelet_down {
    $ROOT_DIR/scripts/kubelet.sh clean
    sleep 30
}

function knative_up {
    $ROOT_DIR/scripts/knative.sh run
    sleep 30
}

function knative_down {
    $ROOT_DIR/scripts/knative.sh clean all force
    sleep 30
}