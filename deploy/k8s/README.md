# deploy/k8s

Kubernetes manifests for running Shiro Distributed System in-cluster.

Apply in order:
```bash
kubectl apply -f deploy/k8s/00-namespace.yaml
kubectl apply -f deploy/k8s/10-configmap.yaml
kubectl apply -f deploy/k8s/11-secret.yaml
kubectl apply -f deploy/k8s/20-deployment.yaml
kubectl apply -f deploy/k8s/30-service.yaml
kubectl apply -f deploy/k8s/40-hpa.yaml
kubectl apply -f deploy/k8s/50-servicemonitor.yaml
```
