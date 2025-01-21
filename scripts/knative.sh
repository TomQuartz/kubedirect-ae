#! /usr/bin/env bash

BASE_DIR=`realpath $(dirname $0)`
ROOT_DIR=$BASE_DIR/..
. $BASE_DIR/util.sh

set -x

function run_knative {
    # serving components
    kubectl apply -f $MANIFESTS_DIR/knative/serving-crds.yaml
    kubectl apply -f $MANIFESTS_DIR/knative/serving-core.yaml
    kubectl apply -f $MANIFESTS_DIR/knative/config-patch.yaml
    
    # read -p "Press enter to continue..."
    sleep 30

    # networking layer
    kubectl apply -f $MANIFESTS_DIR/knative/kourier.yaml
    kubectl patch configmap/config-network \
        --namespace knative-serving \
        --type merge \
        --patch '{"data":{"ingress-class":"kourier.ingress.networking.knative.dev"}}'
    # config dns
    gateway_ip=$(kubectl get service -n kourier-system kourier -o jsonpath='{.spec.clusterIP}')
    kubectl patch configmap/config-domain \
        --namespace knative-serving \
        --type merge \
        --patch '{"data":{"'$gateway_ip'.sslip.io":""}}'
}

# usage: knative.sh clean [all] [force]
function clean_knative {
    if [ "$1" == "all" ]; then
        all=1
        shift
    fi
    if [ "$1" == "force" ]; then
        force=1
        shift
    fi

    kubectl delete ksvc --all || true
    if [ -n "$all" ]; then
        sleep 30
        kubectl delete -f $MANIFESTS_DIR/knative/kourier.yaml
        kubectl delete -f $MANIFESTS_DIR/knative/serving-core.yaml
        sleep 30
        kubectl delete -f $MANIFESTS_DIR/knative/serving-crds.yaml
        sleep 30
        if [ -n "$force" ]; then
            kubectl get pods -A | grep Terminating | awk '{print $1, $2}' | while read -r ns p; do
                kubectl delete pod -n $ns $p --grace-period=0 --force
            done
        fi
    fi
}

case "$1" in
run)
    run_knative
    ;;
clean)
    # clean [all] [force]
    shift
    clean_knative $@
    ;;
esac