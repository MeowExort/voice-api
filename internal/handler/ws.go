package handler

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// WSEvent represents a WebSocket message envelope
type WSEvent struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// Client represents a single WebSocket connection
type Client struct {
	hub      *Hub
	conn     *websocket.Conn
	send     chan []byte
	userID   string
	channels map[string]bool // subscribed channel IDs
	mu       sync.RWMutex
}

// Hub manages all WebSocket clients and Redis pub/sub
type Hub struct {
	clients    map[*Client]bool
	register   chan *Client
	unregister chan *Client
	broadcast  chan *channelMessage
	rdb        *redis.Client
	mu         sync.RWMutex
}

type channelMessage struct {
	ChannelID string
	Data      []byte
}

// redisPubSubMessage is the envelope for cross-replica sync
type redisPubSubMessage struct {
	ChannelID string `json:"channel_id"`
	Payload   string `json:"payload"`
}

func NewHub(rdb *redis.Client) *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan *channelMessage, 256),
		rdb:        rdb,
	}
}

func (hub *Hub) Run() {
	// Subscribe to Redis pub/sub for cross-replica sync
	go hub.subscribeRedis()

	for {
		select {
		case client := <-hub.register:
			hub.mu.Lock()
			hub.clients[client] = true
			hub.mu.Unlock()

		case client := <-hub.unregister:
			hub.mu.Lock()
			if _, ok := hub.clients[client]; ok {
				delete(hub.clients, client)
				close(client.send)
			}
			hub.mu.Unlock()

		case msg := <-hub.broadcast:
			hub.mu.RLock()
			for client := range hub.clients {
				client.mu.RLock()
				subscribed := client.channels[msg.ChannelID]
				client.mu.RUnlock()
				if subscribed {
					select {
					case client.send <- msg.Data:
					default:
						go func(c *Client) {
							hub.unregister <- c
						}(client)
					}
				}
			}
			hub.mu.RUnlock()
		}
	}
}

func (hub *Hub) subscribeRedis() {
	ctx := context.Background()
	pubsub := hub.rdb.Subscribe(ctx, "voice:messages")
	defer pubsub.Close()

	ch := pubsub.Channel()
	for msg := range ch {
		var rsm redisPubSubMessage
		if err := json.Unmarshal([]byte(msg.Payload), &rsm); err != nil {
			continue
		}
		hub.mu.RLock()
		for client := range hub.clients {
			client.mu.RLock()
			subscribed := client.channels[rsm.ChannelID]
			client.mu.RUnlock()
			if subscribed {
				select {
				case client.send <- []byte(rsm.Payload):
				default:
				}
			}
		}
		hub.mu.RUnlock()
	}
}

// PublishToChannel sends a message to all subscribers of a channel (local + Redis)
func (hub *Hub) PublishToChannel(channelID string, event WSEvent) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}

	// Local broadcast
	hub.broadcast <- &channelMessage{ChannelID: channelID, Data: data}

	// Redis pub/sub for other replicas
	rsm := redisPubSubMessage{ChannelID: channelID, Payload: string(data)}
	rsmData, _ := json.Marshal(rsm)
	hub.rdb.Publish(context.Background(), "voice:messages", string(rsmData))
}

// GetOnlineUserIDs returns user IDs currently connected to a channel
func (hub *Hub) GetOnlineUserIDs(channelID string) []string {
	hub.mu.RLock()
	defer hub.mu.RUnlock()

	seen := make(map[string]bool)
	var ids []string
	for client := range hub.clients {
		client.mu.RLock()
		subscribed := client.channels[channelID]
		client.mu.RUnlock()
		if subscribed && !seen[client.userID] {
			seen[client.userID] = true
			ids = append(ids, client.userID)
		}
	}
	return ids
}

func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(65536)
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			break
		}

		var event WSEvent
		if err := json.Unmarshal(message, &event); err != nil {
			continue
		}

		switch event.Type {
		case "subscribe":
			var sub struct {
				ChannelID string `json:"channel_id"`
			}
			if err := json.Unmarshal(event.Data, &sub); err == nil && sub.ChannelID != "" {
				c.mu.Lock()
				c.channels[sub.ChannelID] = true
				c.mu.Unlock()
			}
		case "unsubscribe":
			var sub struct {
				ChannelID string `json:"channel_id"`
			}
			if err := json.Unmarshal(event.Data, &sub); err == nil {
				c.mu.Lock()
				delete(c.channels, sub.ChannelID)
				c.mu.Unlock()
			}
		case "typing":
			var t struct {
				ChannelID string `json:"channel_id"`
			}
			if err := json.Unmarshal(event.Data, &t); err == nil {
				typingData, _ := json.Marshal(gin.H{"user_id": c.userID, "channel_id": t.ChannelID})
				typingEvent := WSEvent{Type: "typing", Data: typingData}
				c.hub.PublishToChannel(t.ChannelID, typingEvent)
			}
		}
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (h *Handler) wsConnect(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("ws upgrade error: %v", err)
		return
	}

	client := &Client{
		hub:      h.hub,
		conn:     conn,
		send:     make(chan []byte, 256),
		userID:   userID,
		channels: make(map[string]bool),
	}

	h.hub.register <- client

	go client.writePump()
	go client.readPump()
}
