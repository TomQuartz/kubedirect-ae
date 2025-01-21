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
    # read -p "Press enter to continue..."
    sleep 20
    # check kourier controller
    for i in $(seq 1 10); do
        local ctrl_pod=$(kubectl get pods -n knative-serving -l app=net-kourier-controller -o jsonpath='{.items[0].metadata.name}')
        if [ -z "$ctrl_pod" ]; then
            echo "net-kourier-controller not found"
            exit 1
        elif [ "$(kubectl get pods -n knative-serving $ctrl_pod -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}')" == "True" ]; then
            echo "net-kourier-controller is ready"
            break
        elif kubectl get pods -n knative-serving $ctrl_pod --no-headers | grep -iq CrashLoopBackOff; then
            echo "net-kourier-controller crashed"
            kubectl delete -n knative-serving pod $ctrl_pod
        fi
        sleep 20
    done
    # check kourier gateway
    for i in $(seq 1 10); do
        local gateway_pods=$(kubectl get pods -n kourier-system -l app=3scale-kourier-gateway -o jsonpath='{.items[*].metadata.name}')
        local desired=0
        local ready=0
        for pod in $gateway_pods; do
            desired=$((desired+1))
            if [ "$(kubectl get pods -n kourier-system $pod -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}')" == "True" ]; then
                ready=$((ready+1))
            elif kubectl get pods -n kourier-system $pod --no-headers | grep -iq CrashLoopBackOff; then
                echo "gateway pod $pod crashed"
                kubectl delete -n kourier-system pod $pod
            fi
        done
        if [ $ready -eq $desired ]; then
            echo "all gateway pods are ready"
            break
        fi
        sleep 20
    done
    sleep 20
    
    # config ingress
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
    kubectl delete cfg --all || true
    kubectl delete rev --all || true
    kubectl delete route --all || true
    
    if [ -n "$all" ]; then
        sleep 30
        timeout 60s kubectl delete -f $MANIFESTS_DIR/knative/kourier.yaml
        timeout 60s kubectl delete -f $MANIFESTS_DIR/knative/serving-core.yaml
        kubectl delete -f $MANIFESTS_DIR/knative/serving-crds.yaml
        if [ -n "$force" ]; then
            sleep 30
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