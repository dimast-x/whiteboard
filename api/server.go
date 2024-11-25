package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type Server struct {
	clients     map[int]*Client
	publisher   message.Publisher
	subscriber  message.Subscriber
	broadcastCh chan []byte
	register    chan *Client
	unregister  chan *Client
	strokes     []Stroke
	shapes      []Shape
	texts       []Text
	nextID      int
	mutex       sync.Mutex
}

func NewServer(publisher message.Publisher, subscriber message.Subscriber) *Server {
	return &Server{
		clients:     make(map[int]*Client),
		publisher:   publisher,
		subscriber:  subscriber,
		broadcastCh: make(chan []byte),
		register:    make(chan *Client),
		unregister:  make(chan *Client),
		strokes:     []Stroke{},
		shapes:      []Shape{},
		texts:       []Text{},
		nextID:      1,
	}
}

func (s *Server) Run() {
	pingTicker := time.NewTicker(30 * time.Second)
	defer pingTicker.Stop()

	batchTicker := time.NewTicker(100 * time.Millisecond)
	defer batchTicker.Stop()

	var messages [][]byte

	for {
		select {
		case client := <-s.register:
			s.onConnect(client)
		case client := <-s.unregister:
			s.onDisconnect(client)
		case data := <-s.broadcastCh:
			messages = append(messages, data)
		case <-batchTicker.C:
			if len(messages) > 0 {
				for _, client := range s.clients {
					for _, msg := range messages {
						select {
						case client.outbound <- msg:
						default:
							close(client.outbound)
							delete(s.clients, client.id)
						}
					}
				}
				messages = nil
			}
		case <-pingTicker.C:
			for _, client := range s.clients {
				if err := client.socket.WriteMessage(websocket.PingMessage, nil); err != nil {
					log.Println("ping error:", err)
					s.unregister <- client
				}
			}
		}
	}
}

func (s *Server) getUsers() []User {
	users := make([]User, 0, len(s.clients))
	for _, c := range s.clients {
		users = append(users, User{ID: c.id, Color: c.color})
	}
	return users
}

func (s *Server) broadcast(protocol interface{}, sender *Client) {
	data, _ := json.Marshal(protocol)
	for _, c := range s.clients {
		if c != sender {
			c.outbound <- data
		}
	}
}

func (s *Server) send(protocol interface{}, client *Client) {
	data, _ := json.Marshal(protocol)
	client.outbound <- data
}

func (s *Server) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	socket, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		http.Error(w, "could not upgrade", http.StatusInternalServerError)
		return
	}
	client := newClient(s, socket)
	s.register <- client

	go client.read()
	go client.write()
}

func (s *Server) onConnect(client *Client) {
	log.Println("client connected: ", client.socket.RemoteAddr())

	s.mutex.Lock()
	defer s.mutex.Unlock()

	client.id = s.nextID
	s.nextID++
	client.color = "#000000"
	s.clients[client.id] = client

	connectedMsg := NewConnected(
		client.color,
		s.getUsers(),
		s.strokes,
		s.shapes,
		s.texts,
	)
	s.send(connectedMsg, client)

	userJoinedMsg := NewUserJoined(client.id, client.color)
	s.broadcast(userJoinedMsg, client)
}

func (s *Server) onDisconnect(client *Client) {
	log.Println("client disconnected: ", client.socket.RemoteAddr())

	s.mutex.Lock()
	defer s.mutex.Unlock()

	client.close()
	delete(s.clients, client.id)

	s.broadcast(NewUserLeft(client.id), nil)
}

func (s *Server) onMessage(data []byte, client *Client) error {
	var baseProtocol struct {
		Kind int `json:"kind"`
	}

	if err := json.Unmarshal(data, &baseProtocol); err != nil {
		return err
	}

	switch baseProtocol.Kind {
	case KindStroke:
		var stroke Stroke
		if err := json.Unmarshal(data, &stroke); err != nil {
			return err
		}
		stroke.UserID = client.id

		s.mutex.Lock()
		s.strokes = append(s.strokes, stroke)
		s.mutex.Unlock()

		payload, _ := json.Marshal(stroke)
		msg := message.NewMessage(watermill.NewUUID(), payload)
		err := s.publisher.Publish("stroke_updates", msg)
		if err != nil {
			log.Println("Error publishing stroke:", err)
		}

	case KindShape:
		var shape Shape
		if err := json.Unmarshal(data, &shape); err != nil {
			return err
		}
		shape.UserID = client.id

		s.mutex.Lock()
		s.shapes = append(s.shapes, shape)
		s.mutex.Unlock()

		payload, _ := json.Marshal(shape)
		msg := message.NewMessage(watermill.NewUUID(), payload)
		err := s.publisher.Publish("shape_updates", msg)
		if err != nil {
			log.Println("Error publishing shape:", err)
		}

	case KindText:
		var text Text
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
		text.UserID = client.id

		s.mutex.Lock()
		s.texts = append(s.texts, text)
		s.mutex.Unlock()

		payload, _ := json.Marshal(text)
		msg := message.NewMessage(watermill.NewUUID(), payload)
		err := s.publisher.Publish("text_updates", msg)
		if err != nil {
			log.Println("Error publishing text:", err)
		}

	case KindClear:
		s.mutex.Lock()
		s.strokes = []Stroke{}
		s.shapes = []Shape{}
		s.texts = []Text{}
		s.mutex.Unlock()

		var clearMsg Clear
		if err := json.Unmarshal(data, &clearMsg); err != nil {
			return err
		}
		clearMsg.UserID = client.id
		payload, _ := json.Marshal(clearMsg)
		msg := message.NewMessage(watermill.NewUUID(), payload)
		err := s.publisher.Publish("clear_updates", msg)
		if err != nil {
			log.Println("Error publishing clear:", err)
		}

	default:
		return errors.New("invalid protocol type")
	}
	return nil
}

func (s *Server) StartSubscribers() {
	strokes, err := s.subscriber.Subscribe(context.Background(), "stroke_updates")
	if err != nil {
		log.Fatal(err)
	}

	go func() {
		for msg := range strokes {
			s.broadcastCh <- msg.Payload
			msg.Ack()
		}
	}()

	shapes, err := s.subscriber.Subscribe(context.Background(), "shape_updates")
	if err != nil {
		log.Fatal(err)
	}
	go func() {
		for msg := range shapes {
			s.broadcastCh <- msg.Payload
			msg.Ack()
		}
	}()

	texts, err := s.subscriber.Subscribe(context.Background(), "text_updates")
	if err != nil {
		log.Fatal(err)
	}
	go func() {
		for msg := range texts {
			s.broadcastCh <- msg.Payload
			msg.Ack()
		}
	}()

	clears, err := s.subscriber.Subscribe(context.Background(), "clear_updates")
	if err != nil {
		log.Fatal(err)
	}
	go func() {
		for msg := range clears {
			s.broadcastCh <- msg.Payload
			msg.Ack()
		}
	}()
}
