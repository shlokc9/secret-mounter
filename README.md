# secret-mounter

Mount previously created secrets just using labels/annotations in a new deployment

## Motivation:

We want to enable users to mount secrets on a workload (deployment/statefulset) automatically if the workload has specific label or annotation set. Users should either be able to mount the entire secret as volume or just a key:value pair. You can make rest of the decision as you want to.

## How it works?

Once the application is running, user can mount secret(s) as a VolumeMount automatically along with a deployment creation.

Just create a secret as you normally would with mandatory label/annotation 'app=secret-mounter' as shown in the sample secret below;
``` {.sourceCode .bash}
apiVersion: v1
kind: Secret
type: Opaque
metadata:
  name: test-secret
  labels:
    app: secret-mounter
stringData:
  name: Shlok Chaudhari
  age: "23"
  designation: Software Engineer
```

And then, create a normal deployment with the same mandatory label/annotation 'app=secret-mounter' as seen above;
``` {.sourceCode .bash}
apiVersion: apps/v1
kind: Deployment
metadata:
  labels:
    app: test-deployment
    app: secret-mounter
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
Resultant deployment will have all keys:values present under path /etc/secret-mounter-data/ inside the pod container

To mount specific keys:values use the optional label/annotation '(secret-name)-secret-keys=key1.key2.key3'. Refer following sample deployment YAML for usage;
``` {.sourceCode .bash}
apiVersion: apps/v1
kind: Deployment
metadata:
  labels:
    app: test-deployment
    app: secret-mounter
    # (secret-name)-secret-keys: key1.key2.key3
    test-secret-secret-keys: name.age
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
Resultant deployment will have mentioned keys:values present under path /etc/secret-mounter-data/ inside the pod container

## How to install secret-mounter?

Prerequisites: A k8s cluster and a kubectl CLI configured to interact with the cluster

Step 1: Download or clone this repository

Step 2: Run following command to install the application on your k8s-cluster

``` {.sourceCode .bash}
> kubectl apply -f secret-mounter/manifests/
```

Step 3: Wait for pods in secret-mounter namespace to reach 'Running' state

## How to test secret-mounter?

Create secret with mandatory labels/annotations

``` {.sourceCode .bash}
> kubectl apply -f secret-mounter/test/test-secret.yaml
```

Create a deployment with mandatory and optional labels/annotations

``` {.sourceCode .bash}
> kubectl apply -f secret-mounter/test/test-deployment.yaml
```

Run the following command to check secrets in the pod for above deployment

``` {.sourceCode .bash}
> kubectl exec -it test-deployment-<hash-value-of-running-pod> -n default -- ls /etc/secret-mounter-data/
```
Mentioned keys in the labels/annotations should be displayed as individual files. Contents to which will be the associated values.

Make sure the deployment is created in the same namespace as the secret would be.

Thank you :)

