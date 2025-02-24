BASE_DIR=`realpath $(dirname $0)`
ROOT_DIR=$BASE_DIR/..

LOG_DIR=${LOG_DIR:-"$ROOT_DIR/log"}
MANIFESTS_DIR=${MANIFESTS_DIR:-"$ROOT_DIR/manifests"}

MY_REPO="shengqipku"
K8S_BUILD_TAG="v1.32.0-kubedirect"

function hosts {
    for n in $(grep -v "localhost" /etc/hosts | awk '{print $NF}'); do
        if [ ! -e "$HOME/.ssh/exclude" ]; then
            echo $n
        elif ! grep -Fxq $n $HOME/.ssh/exclude; then
            echo $n
        fi
    done
}

function workers {
    if [ -z "$1" ]; then
        echo $(hosts | grep -vw $(hostname))
    else
        echo $(hosts | grep -vw $(hostname) | head -n $1)
    fi
}

function docker_images {
    docker images --format '{{.Repository}}:{{.Tag}}'
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
