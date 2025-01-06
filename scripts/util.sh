BASE_DIR=`realpath $(dirname $0)`
ROOT_DIR=$BASE_DIR/..

LOG_DIR=${LOG_DIR:-"$ROOT_DIR/log"}
MANIFESTS_DIR=${MANIFESTS_DIR:-"$ROOT_DIR/manifests"}

MY_REPO="shengqipku"
K8S_BUILD_TAG="v1.31.0-kubedirect"

function hosts {
    grep -v "localhost" /etc/hosts | awk '{print $NF}'
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
