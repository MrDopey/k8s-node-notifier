# Pre-amble

Running workloads in your k8s cluster on an auto-scaling nodepool, but people keep leaving things running? e.g. Jupyter notebook, or `kube-system` pods accidentally get moved to your auto-scaling node (I'm looking at you metrics-server). 

This is an attempt to gain visibility to the k8s cluster nodes and prevent bill-shock.

It is an operator that looks at the node's creation time and shoots a slack message every hour they are running grouped by a label selector.

Note: An alternative method could be to view this at a node level, and implemented as a DaemonSet, though that depends on your use case, as this version looks at a grouping of nodes. This is mostly for me to get familiar with writing a k8s operator


# Minikube testing

```shell
minikube start --feature-gates=WatchList=true
```

# Reference

- https://github.com/wardviaene/golang-demos/tree/main/kubernetes-operator
- https://blog.mimacom.com/k8s-watch-resources/
- https://api.slack.com/messaging/webhooks