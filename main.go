package main

import (
	"log"
	"net/http"

	"github.com/dimast-x/whiteboard/api"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill-redisstream/pkg/redisstream"
	"github.com/redis/go-redis/v9"
)

func main() {
	redisOpts, err := redis.ParseURL("redis://localhost:6379")
	if err != nil {
		panic(err)
	}

	redisClient := redis.NewClient(redisOpts)

	logger := watermill.NewStdLogger(true, true)
	publisher, err := redisstream.NewPublisher(redisstream.PublisherConfig{
		Client: redisClient,
	}, logger)
	if err != nil {
		panic(err)
	}

	subscriber, err := redisstream.NewSubscriber(redisstream.SubscriberConfig{
		Client: redisClient,
	}, logger)
	if err != nil {
		panic(err)
	}

	defer func() {
		if err := publisher.Close(); err != nil {
			log.Println("Error closing publisher:", err)
		}
		if err := subscriber.Close(); err != nil {
			log.Println("Error closing subscriber:", err)
		}
	}()

	server := api.NewServer(publisher, subscriber)
	go server.StartSubscribers()
	go server.Run()

	log.Println("Server is running...")
	http.HandleFunc("/ws", server.HandleWebSocket)
	err = http.ListenAndServe(":3000", nil)
	if err != nil {
		log.Fatal(err)
	}
}
