# secret-sneaker

Sneak secret file automatically while creating a deployment

## Motivation:

We want to enable users to mount secrets on a workload (deployment/statefulset) automatically if the workload has specific label or annotation set. Users should either be able to mount the entire secret as volume or just a key:value pair. You can make rest of the decision as you want to.


