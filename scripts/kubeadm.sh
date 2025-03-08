#! /usr/bin/env bash

BASE_DIR=`realpath $(dirname $0)`
ROOT_DIR=$BASE_DIR/..
. $BASE_DIR/util.sh

set -ex

# usage: kubeadm.sh run [large] [debug] [#workers]
function run_kubeadm {
    if [ "$1" == "large" ]; then
        large=".large"
        shift
    fi
    if [ "$1" == "debug" ]; then
        debug=".debug"
        shift
    fi
    n_workers=$1

    INIT_CONFIG=init$large$debug.yaml
    CNI_CONFIG=flannel$large.yaml
    JOIN_CONFIG=join.yaml

    mkdir -p $LOG_DIR/kubeadm
    sudo rm -rf $LOG_DIR/kubeadm/*
    rm -rf $MANIFESTS_DIR/kubeadm/_tmp_*

    if [ -n "$large" ]; then
        sudo mkdir -p /etc/kubernetes/manifests
        sudo cp $MANIFESTS_DIR/kubeadm/kube-scheduler-config.large.yaml /etc/kubernetes/manifests/
    fi

    # # ensure ports
    # critical_ports=("6443" "2379" "2380" "10250" "10251" "10252" "10256" "10257" "4443")
    # for port in ${critical_ports[@]} ; do
    #     pid=$(sudo lsof -t -i :$port) || continue
    #     if [ -n "$pid" ]; then
    #         echo "port $port is in use by $pid"
    #         sudo kill -9 $pid
    #     fi
    # done

    # kubeadm init
    master_ip=$(grep -w $(hostname) /etc/hosts | awk '{print $1}')
    MASTER_IP=$master_ip envsubst < $MANIFESTS_DIR/kubeadm/$INIT_CONFIG > $MANIFESTS_DIR/kubeadm/_tmp_.$INIT_CONFIG
    sudo kubeadm init --config $MANIFESTS_DIR/kubeadm/_tmp_.$INIT_CONFIG 2>&1 | tee $ROOT_DIR/init.log

    rm -rf $HOME/.kube
    mkdir -p $HOME/.kube
    sudo cp -i /etc/kubernetes/admin.conf $HOME/.kube/config
    sudo chown $(id -u):$(id -g) $HOME/.kube/config

    # check for crashloop
    sleep 10
    crashed=$(kubectl get pods -n kube-system --no-headers | grep -i CrashLoopBackOff) || true
    if [ -n "$crashed" ]; then
        echo "kubeadm init failed: $crashed"
        exit 1
    fi

    # apply and check cni
    kubectl apply -f $MANIFESTS_DIR/kubeadm/$CNI_CONFIG
    sleep 30
    api_endpoint=$(cat $ROOT_DIR/init.log | grep -oP '(?<=kubeadm join )[^\s]*' | head -n 1)
    token=$(cat $ROOT_DIR/init.log | grep -oP '(?<=--token )[^\s]*' | head -n 1)
    token_hash=$(cat $ROOT_DIR/init.log | grep -oP '(?<=--discovery-token-ca-cert-hash )[^\s]*' | head -n 1)

    API_ENDPOINT=$api_endpoint TOKEN=$token TOKEN_HASH=$token_hash \
        envsubst < $MANIFESTS_DIR/kubeadm/$JOIN_CONFIG > $MANIFESTS_DIR/kubeadm/_tmp_.$JOIN_CONFIG
    
    for worker in $(workers $n_workers); do
        echo "joining $worker"
        scp $MANIFESTS_DIR/kubeadm/_tmp_.$JOIN_CONFIG $worker:~/.kubedirect/kubeadm.join.yaml
        ssh $worker -- sudo kubeadm join $api_endpoint --config ~/.kubedirect/kubeadm.join.yaml
        # ssh $worker -- sudo kubeadm join $api_endpoint --token $token --discovery-token-ca-cert-hash $token_hash
    done

    # cp kubeconfig to all workers
    for worker in $(workers $n_workers); do
        ssh $worker -- rm -rf ~/.kube
        scp -qr $HOME/.kube $worker:~
    done

    # install metrics-server
    if [ -z "$large" ]; then
        sleep 30
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
    nohup kubectl logs -n kube-system kube-controller-manager-$master_node --follow >$WATCH_LOG/controller.log 2>&1 &
    pid=$!
    echo "$pid: controller-manager -> $WATCH_LOG/controller.log"
    echo $pid >> $WATCH_DIR/pids
    
    # kube-scheduler
    nohup kubectl logs -n kube-system kube-scheduler-$master_node --follow >$WATCH_LOG/scheduler.log 2>&1 &
    pid=$!
    echo "$pid: scheduler -> $WATCH_LOG/scheduler.log"
    echo $pid >> $WATCH_DIR/pids
}

function watch_kubelet {
    target="kubeadm"
    WATCH_DIR=$ROOT_DIR/watch/$target
    WATCH_LOG=$LOG_DIR/$target/kubelet
    mkdir -p $WATCH_DIR && mkdir -p $WATCH_LOG

    for worker in $(workers $1); do
        nohup ssh $worker "sudo journalctl -u kubelet --follow" >$WATCH_LOG/kubelet-$worker.log 2>&1 &
        pid=$!
        echo "$pid: $worker kubelet -> $WATCH_LOG/kubelet-$worker.log"
        echo $pid >> $WATCH_DIR/pids
    done
    sleep 5
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
        ssh -q $host -- <<EOF &
            sudo kubeadm reset -f
            sleep 30
            sudo journalctl --rotate --vacuum-time=1s
            sudo rm -rf /var/lib/kubelet/*
            sudo rm -rf /var/lib/etcd/*
            sudo rm -rf /run/flannel/*
            # NOTE: the following two dirs cannot be cleaned by rm -rf /*
            sudo rm -rf /var/lib/cni
            sudo rm -rf /etc/cni
            sudo ifconfig flannel.1 down
            sudo ip link delete flannel.1
            sudo ifconfig cni0 down
            sudo brctl delbr cni0
            sudo ip link delete cni0
            sudo iptables -F
            sudo iptables -t nat -F
            sudo iptables -t mangle -F
            sudo iptables -X
            sleep 30
            sudo systemctl restart docker.service docker.socket containerd.service
            sudo setfacl -m "user:$USER:rw" /var/run/docker.sock
            rm -rf ~/.kube
EOF
    done
    wait
}

case "$1" in
run)
    # run [large] [debug] [#workers]
    shift
    run_kubeadm $@
    ;;
watch)
    # watch [ctrl|kubelet|clean] [ctrl:master_name|kubelet:#workers]
    shift
    if [ "$1" == "ctrl" ]; then
        shift
        watch_control_plane $@
    elif [ "$1" == "kubelet" ]; then
        shift
        watch_kubelet $@
    elif [ "$1" == "clean" ]; then
        clean_watch
    fi
    ;;
clean)
    clean_kubeadm
    ;;
test)
    shift
    run_kubeadm $1 debug
    watch_control_plane
    watch_kubelet
    ;;
esac