# Distributed Whiteboard Application

1. Deploy the application:

```docker-compose -f ./compose/backend.yml uo```

2. Run the frontend

Open `frontend/index.html`.

3. Stop the application:

```docker-compose -f ./compose/backend.yml down```

Credits to Tin for a great article that has helped to set up the foundation.
https://outcrawl.com/realtime-collaborative-drawing-go

Before deployment make sure k8s has metrics-server enabled.

```kubectl apply -f https://github.com/kubernetes-sigs/metrics-server/releases/latest/download/components.yaml```

```kubectl -n kube-system edit deployment metrics-server```
```- --kubelet-insecure-tls```