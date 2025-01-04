#! /usr/bin/env bash

BASE_DIR=`realpath $(dirname $0)`
ROOT_DIR=$BASE_DIR/..
. $BASE_DIR/util.sh

set -ex

# usage: kubeadm.sh run [large|debug] [#workers]
function run_kubeadm {
    INIT_CONFIG=init.yaml
    JOIN_CONFIG=join.yaml
    CNI_CONFIG=flannel.yaml

    mode=$1
    case "$mode" in
    large)
        shift
        INIT_CONFIG=init.large.yaml
        CNI_CONFIG=flannel.large.yaml
        ;;
    debug)
        shift
        INIT_CONFIG=init.debug.yaml
        ;;
    esac
    nWorkers=$1

    mkdir -p $LOG_DIR/kubeadm
    sudo rm -rf $LOG_DIR/kubeadm/*

    # kubeadm init
    sudo kubeadm init --config $MANIFESTS_DIR/kubeadm/$INIT_CONFIG 2>&1 | tee $ROOT_DIR/init.log

    mkdir -p $HOME/.kube
    sudo cp -i /etc/kubernetes/admin.conf $HOME/.kube/config
    sudo chown $(id -u):$(id -g) $HOME/.kube/config

    kubectl apply -f $MANIFESTS_DIR/kubeadm/$CNI_CONFIG
    # read -p "Press enter to continue..."
    sleep 30

    api_endpoint=$(cat $ROOT_DIR/init.log | grep -oP '(?<=kubeadm join )[^\s]*' | head -n 1)
    token=$(cat $ROOT_DIR/init.log | grep -oP '(?<=--token )[^\s]*' | head -n 1)
    token_hash=$(cat $ROOT_DIR/init.log | grep -oP '(?<=--discovery-token-ca-cert-hash )[^\s]*' | head -n 1)

    API_ENDPOINT=$api_endpoint TOKEN=$token TOKEN_HASH=$token_hash \
        envsubst < $MANIFESTS_DIR/kubeadm/$JOIN_CONFIG > $MANIFESTS_DIR/kubeadm/_tmp_.$JOIN_CONFIG
    
    for worker in $(workers $nWorkers); do
        echo "joining $worker"
        scp $MANIFESTS_DIR/kubeadm/_tmp_.$JOIN_CONFIG $worker:~/.kubedirect/kubeadm.join.yaml
        ssh $worker -- sudo kubeadm join $api_endpoint --config ~/.kubedirect/kubeadm.join.yaml
        # ssh $worker -- sudo kubeadm join $api_endpoint --token $token --discovery-token-ca-cert-hash $token_hash
    done

    # install metrics-server
    if [ "$mode" != "large" ]; then
        kubectl apply -f https://github.com/kubernetes-sigs/metrics-server/releases/download/v0.7.1/components.yaml
        kubectl patch -n kube-system deployment metrics-server \
            --type='json' \
            -p='[{"op": "replace", "path": "/spec/template/spec/containers/0/args/2", "value": "--kubelet-preferred-address-types=InternalIP"},
                {"op": "add", "path": "/spec/template/spec/containers/0/args/-", "value": "--kubelet-insecure-tls"}]'
    fi
}

function watch_control_plane {
    target="kubeadm"
    master_node=${1:-"$(hostname)"}

    WATCH_DIR=$ROOT_DIR/watch/$target
    WATCH_LOG=$LOG_DIR/$target
    mkdir -p $WATCH_DIR && mkdir -p $WATCH_LOG

    # kube-controller-manager
    kubectl logs -n kube-system kube-controller-manager-$master_node --follow >$WATCH_LOG/controller.log 2>&1 &
    pid=$!
    echo "$pid: controller-manager -> $WATCH_LOG/controller.log"
    echo $pid >> $WATCH_DIR/pids
    
    # kube-apiserver
    kubectl logs -n kube-system kube-apiserver-$master_node --follow >$WATCH_LOG/apiserver.log 2>&1 &
    pid=$!
    echo "$!: apiserver -> $WATCH_LOG/apiserver.log"
    echo $pid >> $WATCH_DIR/pids
}

function watch_kubelet {
    target="kubeadm"
    WATCH_DIR=$ROOT_DIR/watch/$target
    WATCH_LOG=$LOG_DIR/$target/kubelet
    mkdir -p $WATCH_DIR && mkdir -p $WATCH_LOG

    for worker in $(workers $1); do
        ssh $worker "sudo journalctl -u kubelet --follow" >$WATCH_LOG/kubelet-$worker.log 2>&1 &
        pid=$!
        echo "$pid: $1 kubelet -> $WATCH_LOG/kubelet-$worker.log"
        echo $pid >> $WATCH_DIR/pids
    done
}

function clean_watch {
    target="kubeadm"
    WATCH_DIR=$ROOT_DIR/watch/$target
    if [ -d "$WATCH_DIR" ]; then
        for pid in $(cat $WATCH_DIR/pids); do
            kill $pid || true
        done
    fi
    rm -rf $WATCH_DIR
}

function clean_kubeadm {
    clean_watch
    for host in $(hosts); do
    {
        ssh -q $host -- <<EOF
            sudo kubeadm reset -f
            sudo systemctl stop kubelet
            sudo systemctl stop containerd
            sudo journalctl --rotate --vacuum-time=1s
            sudo rm -rf /etc/kubernetes/*
            sudo rm -rf /var/lib/kubelet/*
            sudo rm -rf /var/lib/etcd/*
            sudo rm -rf /etc/cni/net.d/*flannel*
            sudo iptables -F
            sudo iptables -t nat -F
            sudo iptables -t mangle -F
            sudo iptables -X
            sudo ifconfig flannel.1 down
            sudo ip link delete flannel.1
            sudo ifconfig cni0 down
            sudo ip link delete cni0
            sudo systemctl restart containerd
EOF
    } &
    done
    wait
    rm -rf ~/.kube/config
    sudo systemctl restart docker.service docker.socket containerd
}

case "$1" in
run)
    # run [large|debug] [#workers]
    shift
    run_kubeadm $@
    ;;
watch)
    # watch [ctrl|kubelet] [ctrl:master_name|kubelet:#workers]
    shift
    if [ "$1" == "ctrl" ]; then
        shift
        watch_control_plane $@
    elif [ "$1" == "kubelet" ]; then
        shift
        watch_kubelet $@
    fi
    ;;
clean)
    clean_kubeadm
    ;;
test)
    ;;
esac