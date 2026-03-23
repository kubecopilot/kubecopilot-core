#!/bin/bash

echo "Uninstalling Kube Copilot Agent..."

kubectl delete -k config/samples/
make undeploy
make uninstall
kubectl delete namespace kube-copilot-agent