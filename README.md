# secret-sneaker

Sneak secret file in your namespace automatically while creating a deployment

## Motivation:

We want to enable users to mount secrets on a workload (deployment/statefulset) automatically if the workload has specific label or annotation set. Users should either be able to mount the entire secret as volume or just a key:value pair. You can make rest of the decision as you want to.

## How it works?

Once the application is running, user can "sneak-in" (create) secret as a VolumeMount automatically along with a deployment creation.

Just update your secrets as a key:value pair at path /mnt/data/secret-sneaker-data/secrets.json on your host machine (Create the entire path if required)

And, mention mandatory label 'app=secret-sneaker' in your manifest before creating a deployment as shown in the sample deployment manifest below;
``` {.sourceCode .bash}
apiVersion: apps/v1
kind: Deployment
metadata:
  labels:
    app: test-deployment
    app: secret-sneaker
  name: test-deployment
spec:
  replicas: 1
  selector:
    matchLabels:
      app: test-deployment
  template:
    metadata:
      labels:
        app: test-deployment
    spec:
      containers:
      - command:
        - ping
        - 8.8.8.8
        image: busybox:latest
        name: busybox
```
Resultant deployment will have secrets present under path /etc/secret-sneaker-data/

## How to install secret-sneaker?

Step 1: Install docker, kind and kubectl

Step 2: Run following command to start a multi-node cluster using kind

``` {.sourceCode .bash}
> kind create cluster --config manifests/kind/cluster-config.yaml
```

Step 3: Run following command to install the application on your k8s-cluster

``` {.sourceCode .bash}
> kubectl apply -f manifests/application/
```

Step 4: Wait for pods in secret-sneaker namespace to reach 'Running' state

## How to test secret-sneaker?

Terminal session 1 - Watch the secrets

``` {.sourceCode .bash}
> kubectl get secrets -n default -w
```

Terminal session 2 - Create a deployment with mandatory label

``` {.sourceCode .bash}
> kubectl apply -f test/
```
You can now see a new secret in Terminal session 1. 

Run the following command to check the secret in the above deployment

``` {.sourceCode .bash}
> kubectl exec -it test-deployment-<hash-value-of-running-pod> -n default -- ls /etc/secret-sneaker-data/
```
Keys mentioned in the secrets.json file should be now be visible as individual files. Contents to which are the associated values.

Note: Make sure you are watching the same namespace where the configmap is created.

Thank you :)

