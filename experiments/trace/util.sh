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
    sleep 60
}

function kubeadm_down {
    $ROOT_DIR/scripts/kubeadm.sh clean
    sleep 30
}

# usage: custom_kubelet_up [watch] [#workers]
function custom_kubelet_up {
    if [ "$1" == "watch" ]; then
        local verbose="-v=2"
    fi
    $ROOT_DIR/scripts/kubelet.sh run $@ -- -ready-after=200 $verbose
    sleep 60
}

function custom_kubelet_down {
    $ROOT_DIR/scripts/kubelet.sh clean
    sleep 30
}

function knative_up {
    $ROOT_DIR/scripts/knative.sh run
    sleep 60
}

function knative_down {
    $ROOT_DIR/scripts/knative.sh clean all force
    sleep 30
}

function wait_for_pods {
    local selector=$1
    if [ -n "$2" ]; then
        local namespace="-n $2"
    fi
    while true; do
        local pods=$(kubectl get pods $namespace -l $selector -o jsonpath='{.items[*].metadata.name}')
        if [ -z "$pods" ]; then
            echo "no pods found"
            return 1
        fi
        local desired=0
        local ready=0
        for pod in $pods; do
            desired=$((desired+1))
            if [ "$(kubectl get pods $namespace $pod -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}')" == "True" ]; then
                ready=$((ready+1))
            elif kubectl get pods $namespace $pod --no-headers | grep -iq CrashLoopBackOff; then
                echo "pod $pod crashed"
                kubectl delete pod $namespace $pod
            fi
        done
        if [ $ready -eq $desired ]; then
            echo "all pods are ready"
            break
        fi
        sleep 20
    done
}