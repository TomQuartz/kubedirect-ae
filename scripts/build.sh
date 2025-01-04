#! /usr/bin/env bash

BASE_DIR=`realpath $(dirname $0)`
ROOT_DIR=$BASE_DIR/..
. $BASE_DIR/util.sh

set -ex

MY_REPO="shengqipku"
K8S_BUILD_TAG="v1.31.0-kubedirect"

function build_kind {
    kind_image=$MY_REPO/kind-node:$K8S_BUILD_TAG
    cd $ROOT_DIR/kubernetes
    if ! git tag -l | grep -q $K8S_BUILD_TAG; then
        git tag $K8S_BUILD_TAG
    fi
    
    kind build node-image . --image $kind_image
    docker push $kind_image
}

function build_kubelet {
    echo "Building kubelet binary..."
    cd $ROOT_DIR/kubernetes
    if ! git tag -l | grep -q $K8S_BUILD_TAG; then
        git tag $K8S_BUILD_TAG
    fi

    make WHAT=cmd/kubelet

    echo "Distributing kubelet binary..."
    for host in $(hosts); do
    {
        scp _output/bin/kubelet $host:~/kubelet # >/dev/null 2>&1
        ssh -q $host -- sudo systemctl stop kubelet
        ssh -q $host -- sudo mv ~/kubelet /usr/bin/kubelet
    } &
    done
    wait
}

function build_k8s {
    echo "Building k8s control plane images..."
    apis_image=$MY_REPO/kube-apiserver:$K8S_BUILD_TAG
    sched_image=$MY_REPO/kube-scheduler:$K8S_BUILD_TAG
    ctrl_image=$MY_REPO/kube-controller-manager:$K8S_BUILD_TAG
    proxy_image=$MY_REPO/kube-proxy:$K8S_BUILD_TAG
    
    cd $ROOT_DIR/kubernetes
    if ! git tag -l | grep -q $K8S_BUILD_TAG; then
        git tag $K8S_BUILD_TAG
    fi
    
    tmp_registry="tmp.k8s.io"
    make quick-release-images KUBE_DOCKER_REGISTRY=$tmp_registry
    apis_image_tmp=$(docker_images | grep $tmp_registry | grep apiserver)
    sched_image_tmp=$(docker_images | grep $tmp_registry | grep scheduler)
    ctrl_image_tmp=$(docker_images | grep $tmp_registry | grep controller)
    proxy_image_tmp=$(docker_images | grep $tmp_registry | grep proxy)
    
    docker tag $apis_image_tmp $apis_image
    docker tag $sched_image_tmp $sched_image
    docker tag $ctrl_image_tmp $ctrl_image
    docker tag $proxy_image_tmp $proxy_image
    docker rmi $(docker_images | grep $tmp_registry)

    docker push $apis_image
    docker push $sched_image
    docker push $ctrl_image
    docker push $proxy_image

    # finally build and distribute kubelet
    build_kubelet
}

function prune {
    docker system prune -f >/dev/null 2>&1
    docker volume prune -f >/dev/null 2>&1
    # kube_build=$(docker_images | grep kube-build) || true
    # if [ "$kube_build" != "" ]; then
    #     docker rmi $kube_build
    # fi
}

case "$1" in
kind)
    build_kind
    ;;
k8s)
    build_k8s
    ;;
kubelet)
    build_kubelet
    ;;
prune)
    prune
    ;;
esac