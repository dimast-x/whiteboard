package api

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
)

type Client struct {
	server   *Server
	id       int
	color    string
	socket   *websocket.Conn
	outbound chan []byte
}

func newClient(server *Server, socket *websocket.Conn) *Client {
	return &Client{
		server:   server,
		socket:   socket,
		outbound: make(chan []byte, 256),
	}
}

func (client *Client) read() {
	defer func() {
		client.server.unregister <- client
	}()

	client.socket.SetReadDeadline(time.Now().Add(60 * time.Second))
	client.socket.SetPongHandler(func(string) error {
		client.socket.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, data, err := client.socket.ReadMessage()
		if err != nil {
			log.Println("read error:", err)
			break
		}
		if err := client.server.onMessage(data, client); err != nil {
			log.Println("error handling message:", err)
		}
	}
}

func (client *Client) write() {
	defer client.close()

	for {
		select {
		case data, ok := <-client.outbound:
			if !ok {
				client.socket.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := client.socket.WriteMessage(websocket.TextMessage, data); err != nil {
				log.Println("write error:", err)
				return
			}
		}
	}
}

func (client *Client) close() {
	client.socket.Close()
	select {
	case <-client.outbound:
	default:
		close(client.outbound)
	}
}

func CreateConsumerGroup(client *redis.ClusterClient, stream, group string) error {
	ctx := context.Background()
	err := client.XGroupCreateMkStream(ctx, stream, group, "0-0").Err()
	if err != nil {
		if err.Error() == "BUSYGROUP Consumer Group name already exists" {
			err = client.XGroupSetID(ctx, stream, group, "0-0").Err()
			if err != nil {
				return fmt.Errorf("failed to reset consumer group ID: %v", err)
			}
		} else {
			return err
		}
	}
	return nil
}
