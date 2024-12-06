package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/dimast-x/whiteboard/api"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill-redisstream/pkg/redisstream"
	"github.com/redis/go-redis/v9"
)

func main() {
	redisClusterOptions := &redis.ClusterOptions{
		Addrs: []string{
			"redis-cl-redis-cluster-0.redis-cl-redis-cluster-headless.default.svc.cluster.local:6379",
			"redis-cl-redis-cluster-1.redis-cl-redis-cluster-headless.default.svc.cluster.local:6379",
			"redis-cl-redis-cluster-2.redis-cl-redis-cluster-headless.default.svc.cluster.local:6379",
			"redis-cl-redis-cluster-3.redis-cl-redis-cluster-headless.default.svc.cluster.local:6379",
			"redis-cl-redis-cluster-4.redis-cl-redis-cluster-headless.default.svc.cluster.local:6379",
			"redis-cl-redis-cluster-5.redis-cl-redis-cluster-headless.default.svc.cluster.local:6379",
		},
		Password: "nxTbgXeWjL", // TODO: take from env
	}

	redisClient := redis.NewClusterClient(redisClusterOptions)

	logger := watermill.NewStdLogger(true, true)
	publisher, err := redisstream.NewPublisher(redisstream.PublisherConfig{
		Client: redisClient,
	}, logger)
	if err != nil {
		log.Fatal("Failed to create publisher:", err)
	}

	subscriber, err := redisstream.NewSubscriber(redisstream.SubscriberConfig{
		Client: redisClient,
	}, logger)
	if err != nil {
		log.Fatal("Failed to create subscriber:", err)
	}

	reconcileConsumerGroup := "reconcile_group"
	topics := []string{"stroke_updates", "shape_updates", "text_updates", "clear_updates"}

	for _, topic := range topics {
		err := api.CreateConsumerGroup(redisClient, topic, reconcileConsumerGroup)
		if err != nil {
			log.Fatalf("Failed to create consumer group for topic %s: %v", topic, err)
		}
	}

	reconcileSubscriber, err := redisstream.NewSubscriber(redisstream.SubscriberConfig{
		Client:        redisClient,
		ConsumerGroup: reconcileConsumerGroup,
		OldestId:      "0-0",
	}, logger)
	if err != nil {
		log.Fatal("Failed to create reconcile subscriber:", err)
	}

	defer func() {
		if err := publisher.Close(); err != nil {
			log.Println("Error closing publisher:", err)
		}
		if err := subscriber.Close(); err != nil {
			log.Println("Error closing subscriber:", err)
		}
		if err := reconcileSubscriber.Close(); err != nil {
			log.Println("Error closing bootstrap subscriber:", err)
		}
	}()

	server := api.NewServer(publisher, subscriber, redisClient)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := server.ReconcileState(ctx, reconcileSubscriber, topics); err != nil {
		log.Fatalf("Failed to bootstrap state: %v", err)
	}

	go server.StartSubscribers()
	go server.Run()

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	http.HandleFunc("/ws", server.HandleWebSocket)

	log.Println("Server is running on :3000. Node ID: ", api.NodeID)
	if err := http.ListenAndServe(":3000", nil); err != nil {
		log.Fatal(err)
	}
}
