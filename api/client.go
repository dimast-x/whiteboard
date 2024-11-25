package api

import (
	"log"
	"time"

	"github.com/gorilla/websocket"
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
		outbound: make(chan []byte),
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
		client.server.onMessage(data, client)
	}
}

func (client *Client) write() {
	defer func() {
		client.socket.Close()
	}()
	for {
		select {
		case data, ok := <-client.outbound:
			if !ok {
				client.socket.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			client.socket.WriteMessage(websocket.TextMessage, data)
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
