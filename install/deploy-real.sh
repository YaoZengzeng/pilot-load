#!/bin/bash

kubectl create ns pilot-load-test

kubectl apply -f load-deployment-real.yaml

kubectl create clusterrolebinding pilot-load --clusterrole=cluster-admin --user=system:serviceaccount:pilot-load-test:pilot-load

kubectl apply -f configs/only-pods-svcs.yaml

