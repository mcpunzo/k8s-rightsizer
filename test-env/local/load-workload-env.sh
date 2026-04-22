#!/bin/bash

kubectl create ns k8s-rightsizer-test

kubectl apply -f ./workload-env.yaml

# add a deployment with an image that doesn't exist to check that the rightsizer can handle it
# kubectl set image deployment/test-standard-rolling nginx=nginx:non-existent -n k8s-rightsizer