# Distributed Whiteboard Application

1. Deploy the application:

```docker-compose -f ./compose/backend.yml uo```

2. Run the frontend

Open `frontend/index.html`.

3. Stop the application:

```docker-compose -f ./compose/backend.yml down```

Credits to Tin for a great article that has helped to set up the foundation.
https://outcrawl.com/realtime-collaborative-drawing-go

Before deployment make sure k8s has metrics-server enabled (required for autoscaling to work).

```kubectl apply -f https://github.com/kubernetes-sigs/metrics-server/releases/latest/download/components.yaml```

```kubectl -n kube-system edit deployment metrics-server```
```- --kubelet-insecure-tls```

## Test Autoscaling

```hey -n 10000 -c 100 http://localhost:30080/ws```

## Redis cluster deployment

    ```helm install redis-cl oci://registry-1.docker.io/bitnamicharts/redis-cluster```

## Persistence

### RDB (Redis Database Backup)

    Snapshot File: /data/dump.rdb

Default configurations is used:

- Save the DB if at least 1 key changes within 900 seconds (15 minutes).
- Save the DB if at least 10 keys change within 300 seconds (5 minutes).
- Save the DB if at least 10,000 keys change within 60 seconds.
-  Use Case: Suitable for backups and scenarios where data loss between snapshots is acceptable.
