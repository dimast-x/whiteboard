package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
)

var (
	NodeID = uuid.New().String()
)

type lwwElement[T any] struct {
	Element   T
	Timestamp time.Time
}

type lwwSet[T any] struct {
	mu       sync.Mutex
	elements map[string]lwwElement[T]
	idGetter func(T) string
}

func newLWWSet[T any](idGetter func(T) string) *lwwSet[T] {
	return &lwwSet[T]{
		elements: make(map[string]lwwElement[T]),
		idGetter: idGetter,
	}
}

func (s *lwwSet[T]) Add(element T, timestamp time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.idGetter(element)
	existing, exists := s.elements[id]
	if !exists || timestamp.After(existing.Timestamp) {
		s.elements[id] = lwwElement[T]{Element: element, Timestamp: timestamp}
	}
}

func (s *lwwSet[T]) ToSlice() []T {
	s.mu.Lock()
	defer s.mu.Unlock()
	res := make([]T, 0, len(s.elements))
	for _, v := range s.elements {
		res = append(res, v.Element)
	}
	return res
}

func (s *lwwSet[T]) Clear(timestamp time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.elements = make(map[string]lwwElement[T])
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type Server struct {
	clients     map[int]*Client
	publisher   message.Publisher
	subscriber  message.Subscriber
	redisClient *redis.ClusterClient
	broadcastCh chan []byte
	register    chan *Client
	unregister  chan *Client
	nextID      int
	mutex       sync.Mutex

	strokeSet *lwwSet[Stroke]
	shapeSet  *lwwSet[Shape]
	textSet   *lwwSet[Text]
}

func NewServer(publisher message.Publisher, subscriber message.Subscriber, redisClient *redis.ClusterClient) *Server {
	return &Server{
		clients:     make(map[int]*Client),
		publisher:   publisher,
		subscriber:  subscriber,
		redisClient: redisClient, // Initialize the new field
		broadcastCh: make(chan []byte, 1024),
		register:    make(chan *Client),
		unregister:  make(chan *Client),
		nextID:      1,
		strokeSet:   newLWWSet[Stroke](func(s Stroke) string { return s.ID }),
		shapeSet:    newLWWSet[Shape](func(sh Shape) string { return sh.ID }),
		textSet:     newLWWSet[Text](func(t Text) string { return t.ID }),
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
	s.mutex.Lock()
	defer s.mutex.Unlock()
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
		log.Println("Upgrade error:", err)
		http.Error(w, "could not upgrade to websocket", http.StatusInternalServerError)
		return
	}
	client := newClient(s, socket)
	s.register <- client

	go client.read()
	go client.write()
}

func (s *Server) RegisterClient(c *Client) {
	s.register <- c
}

func (s *Server) UnregisterClient(c *Client) {
	s.unregister <- c
}

func (s *Server) onConnect(client *Client) {
	log.Println("Client connected:", client.socket.RemoteAddr())

	s.mutex.Lock()
	client.id = s.nextID
	s.nextID++
	client.color = "#000000"
	s.clients[client.id] = client
	s.mutex.Unlock()

	connectedMsg := NewConnected(
		client.color,
		s.getUsers(),
		s.strokeSet.ToSlice(),
		s.shapeSet.ToSlice(),
		s.textSet.ToSlice(),
	)
	s.send(connectedMsg, client)

	userJoinedMsg := NewUserJoined(client.id, client.color)
	s.broadcast(userJoinedMsg, client)
}

func (s *Server) onDisconnect(client *Client) {
	log.Println("Client disconnected:", client.socket.RemoteAddr())

	s.mutex.Lock()
	defer s.mutex.Unlock()
	client.close()
	delete(s.clients, client.id)

	s.broadcast(NewUserLeft(client.id), nil)
}

func (s *Server) onMessage(data []byte, client *Client) error {
	var base struct {
		Kind int `json:"kind"`
	}

	if err := json.Unmarshal(data, &base); err != nil {
		return err
	}

	switch base.Kind {
	case KindStroke:
		var stroke Stroke
		if err := json.Unmarshal(data, &stroke); err != nil {
			return err
		}
		stroke.UserID = client.id
		if stroke.ID == "" {
			stroke.ID = watermill.NewUUID()
		}
		return s.handleStroke(stroke)

	case KindShape:
		var shape Shape
		if err := json.Unmarshal(data, &shape); err != nil {
			return err
		}
		shape.UserID = client.id
		if shape.ID == "" {
			shape.ID = watermill.NewUUID()
		}
		return s.handleShape(shape)

	case KindText:
		var text Text
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
		text.UserID = client.id
		if text.ID == "" {
			text.ID = watermill.NewUUID()
		}
		return s.handleText(text)

	case KindClear:
		var clearMsg Clear
		if err := json.Unmarshal(data, &clearMsg); err != nil {
			return err
		}
		clearMsg.UserID = client.id
		if clearMsg.ID == "" {
			clearMsg.ID = watermill.NewUUID()
		}
		return s.handleClear(clearMsg)

	default:
		return errors.New("invalid protocol type")
	}
}

func (s *Server) handleStroke(stroke Stroke) error {
	payload, err := json.Marshal(stroke)
	if err != nil {
		return err
	}
	msg := message.NewMessage(watermill.NewUUID(), payload)
	err = s.publisher.Publish("stroke_updates", msg)
	if err != nil {
		log.Println("Error publishing stroke:", err)
		return err
	}
	return nil
}

func (s *Server) handleShape(shape Shape) error {
	payload, err := json.Marshal(shape)
	if err != nil {
		return err
	}
	msg := message.NewMessage(watermill.NewUUID(), payload)
	err = s.publisher.Publish("shape_updates", msg)
	if err != nil {
		log.Println("Error publishing shape:", err)
		return err
	}
	return nil
}

func (s *Server) handleText(text Text) error {
	payload, err := json.Marshal(text)
	if err != nil {
		return err
	}
	msg := message.NewMessage(watermill.NewUUID(), payload)
	err = s.publisher.Publish("text_updates", msg)
	if err != nil {
		log.Println("Error publishing text:", err)
		return err
	}
	return nil
}

func (s *Server) handleClear(clearMsg Clear) error {
	payload, err := json.Marshal(clearMsg)
	if err != nil {
		return err
	}
	msg := message.NewMessage(watermill.NewUUID(), payload)
	err = s.publisher.Publish("clear_updates", msg)
	if err != nil {
		log.Println("Error publishing clear:", err)
		return err
	}

	now := time.Now().UTC()
	s.strokeSet.Clear(now)
	s.shapeSet.Clear(now)
	s.textSet.Clear(now)
	log.Println("Cleared local CRDT sets")

	streams := []string{"stroke_updates", "shape_updates", "text_updates", "clear_updates"}

	for _, stream := range streams {
		err := s.redisClient.Del(context.Background(), stream).Err()
		if err != nil {
			log.Printf("Error deleting stream %s: %v", stream, err)
			return err
		}
		log.Printf("Deleted stream: %s", stream)

		err = CreateConsumerGroup(s.redisClient, stream, "reconcile_group")
		if err != nil {
			log.Printf("Error recreating consumer group for stream %s: %v", stream, err)
			return err
		}
		log.Printf("Recreated consumer group 'reconcile_group' for stream: %s", stream)
	}

	return nil
}

func (s *Server) StartSubscribers() {
	strokes, err := s.subscriber.Subscribe(context.Background(), "stroke_updates")
	if err != nil {
		log.Fatal("Failed to subscribe to stroke_updates:", err)
	}
	go s.handleSubscription(strokes, func(data []byte) error {
		var stroke Stroke
		if err := json.Unmarshal(data, &stroke); err != nil {
			return err
		}
		s.strokeSet.Add(stroke, time.Now().UTC())
		return nil
	})

	shapes, err := s.subscriber.Subscribe(context.Background(), "shape_updates")
	if err != nil {
		log.Fatal("Failed to subscribe to shape_updates:", err)
	}
	go s.handleSubscription(shapes, func(data []byte) error {
		var shape Shape
		if err := json.Unmarshal(data, &shape); err != nil {
			return err
		}
		s.shapeSet.Add(shape, time.Now().UTC())
		return nil
	})

	texts, err := s.subscriber.Subscribe(context.Background(), "text_updates")
	if err != nil {
		log.Fatal("Failed to subscribe to text_updates:", err)
	}
	go s.handleSubscription(texts, func(data []byte) error {
		var text Text
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
		s.textSet.Add(text, time.Now().UTC())
		return nil
	})

	clears, err := s.subscriber.Subscribe(context.Background(), "clear_updates")
	if err != nil {
		log.Fatal("Failed to subscribe to clear_updates:", err)
	}
	go s.handleSubscription(clears, func(data []byte) error {
		var clearMsg Clear
		if err := json.Unmarshal(data, &clearMsg); err != nil {
			return err
		}

		s.strokeSet.Clear(time.Now().UTC())
		s.shapeSet.Clear(time.Now().UTC())
		s.textSet.Clear(time.Now().UTC())
		return nil
	})
}

func (s *Server) handleSubscription(msgs <-chan *message.Message, applyFunc func([]byte) error) {
	for msg := range msgs {
		if err := applyFunc(msg.Payload); err != nil {
			log.Println("Error applying message:", err)
			msg.Nack()
			continue
		}
		s.broadcastCh <- msg.Payload
		msg.Ack()
	}
}

func (s *Server) ReconcileState(ctx context.Context, reconcileSubscriber message.Subscriber, topics []string) error {
	for _, topic := range topics {
		msgChan, err := reconcileSubscriber.Subscribe(ctx, topic)
		if err != nil {
			return err
		}

		log.Println("Reconciling topic:", topic)

		inactivityTimeout := 50 * time.Millisecond
		inactivityTimer := time.NewTimer(inactivityTimeout)

		for {
			select {
			case msg, ok := <-msgChan:
				if !ok {
					log.Println("Subscription channel closed for topic:", topic)
					goto NextTopic
				}
				if err := s.applyMessage(msg); err != nil {
					log.Println("Error applying reconcile message:", err)
				}
				msg.Ack()

				if !inactivityTimer.Stop() {
					<-inactivityTimer.C
				}
				inactivityTimer.Reset(inactivityTimeout)

			case <-inactivityTimer.C:
				log.Println("No more messages in topic:", topic)
				goto NextTopic

			case <-ctx.Done():
				log.Println("Reconcile context done, stopping early")
				return ctx.Err()
			}
		}

	NextTopic:
		inactivityTimer.Stop()
	}

	log.Println("Reconciliation completed")
	return nil
}

func (s *Server) applyMessage(msg *message.Message) error {
	fmt.Println("message received")

	var base struct {
		Kind int `json:"kind"`
	}
	if err := json.Unmarshal(msg.Payload, &base); err != nil {
		return err
	}

	now := time.Now().UTC()

	switch base.Kind {
	case KindStroke:
		var stroke Stroke
		if err := json.Unmarshal(msg.Payload, &stroke); err != nil {
			return err
		}
		if stroke.ID == "" || stroke.UserID == 0 || len(stroke.Points) == 0 {
			log.Println("Invalid stroke data, skipping")
			return nil
		}
		s.strokeSet.Add(stroke, now)
	case KindShape:
		var shape Shape
		if err := json.Unmarshal(msg.Payload, &shape); err != nil {
			return err
		}
		if shape.ID == "" || shape.UserID == 0 || shape.ShapeType == "" || shape.Color == "" {
			log.Println("Invalid shape data, skipping")
			return nil
		}
		s.shapeSet.Add(shape, now)
	case KindText:
		var text Text
		if err := json.Unmarshal(msg.Payload, &text); err != nil {
			return err
		}
		if text.ID == "" || text.UserID == 0 || text.Content == "" || text.Color == "" {
			log.Println("Invalid text data, skipping")
			return nil
		}
		s.textSet.Add(text, now)
	case KindClear:
		s.strokeSet.Clear(now)
		s.shapeSet.Clear(now)
		s.textSet.Clear(now)
	default:
		log.Println("Unknown message kind during reconcile:", base.Kind)
		return nil
	}
	return nil
}
