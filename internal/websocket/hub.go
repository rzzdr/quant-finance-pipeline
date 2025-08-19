package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rzzdr/quant-finance-pipeline/internal/market"
	"github.com/rzzdr/quant-finance-pipeline/internal/orderbook"
	"github.com/rzzdr/quant-finance-pipeline/pkg/utils/logger"
)

// Hub maintains the set of active clients and broadcasts messages to them
type Hub struct {
	clients       map[*Client]bool
	broadcast     chan []byte
	register      chan *Client
	unregister    chan *Client
	subscriptions map[string]map[*Client]bool // symbol -> clients
	processor     *market.EnhancedProcessor
	log           *logger.Logger
	mu            sync.RWMutex
}

// Client is a middleman between the websocket connection and the hub
type Client struct {
	hub           *Hub
	conn          *websocket.Conn
	send          chan []byte
	id            string
	subscriptions map[string]bool // symbols this client is subscribed to
	mu            sync.RWMutex
}

// Message represents a WebSocket message
type Message struct {
	Type   string      `json:"type"`
	Symbol string      `json:"symbol,omitempty"`
	Data   interface{} `json:"data,omitempty"`
	Error  string      `json:"error,omitempty"`
	ID     string      `json:"id,omitempty"`
}

// Subscription request message
type SubscriptionMessage struct {
	Type    string   `json:"type"`
	Symbols []string `json:"symbols"`
	Feeds   []string `json:"feeds"` // orderbook, trades, quotes
	ID      string   `json:"id,omitempty"`
}

// WebSocket upgrader
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// In production, implement proper origin checking
		return true
	},
}

const (
	// Time allowed to write a message to the peer
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer
	maxMessageSize = 512
)

// NewHub creates a new WebSocket hub
func NewHub(processor *market.EnhancedProcessor) *Hub {
	return &Hub{
		clients:       make(map[*Client]bool),
		broadcast:     make(chan []byte, 256),
		register:      make(chan *Client),
		unregister:    make(chan *Client),
		subscriptions: make(map[string]map[*Client]bool),
		processor:     processor,
		log:           logger.GetLogger("websocket.hub"),
	}
}

// Run starts the WebSocket hub
func (h *Hub) Run(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	h.log.Info("Starting WebSocket hub")

	for {
		select {
		case <-ctx.Done():
			h.log.Info("WebSocket hub shutting down")
			return

		case client := <-h.register:
			h.clients[client] = true
			h.log.Infof("Client %s registered", client.id)

		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
				h.removeClientSubscriptions(client)
				h.log.Infof("Client %s unregistered", client.id)
			}

		case message := <-h.broadcast:
			h.broadcastToClients(message)

		case <-ticker.C:
			h.broadcastMarketUpdates()
		}
	}
}

// HandleWebSocket handles WebSocket upgrade and client management
func (h *Hub) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.log.Errorf("WebSocket upgrade failed: %v", err)
		return
	}

	client := &Client{
		hub:           h,
		conn:          conn,
		send:          make(chan []byte, 256),
		id:            generateClientID(),
		subscriptions: make(map[string]bool),
	}

	client.hub.register <- client

	// Start client goroutines
	go client.writePump()
	go client.readPump()
}

// readPump pumps messages from the websocket connection to the hub
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, messageData, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.hub.log.Errorf("WebSocket error: %v", err)
			}
			break
		}

		c.handleMessage(messageData)
	}
}

// writePump pumps messages from the hub to the websocket connection
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// Add queued messages to the current message
			n := len(c.send)
			for i := 0; i < n; i++ {
				w.Write([]byte("\n"))
				w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// handleMessage handles incoming messages from the client
func (c *Client) handleMessage(messageData []byte) {
	var msg SubscriptionMessage
	if err := json.Unmarshal(messageData, &msg); err != nil {
		c.sendError("Invalid message format")
		return
	}

	switch msg.Type {
	case "subscribe":
		c.handleSubscription(msg)
	case "unsubscribe":
		c.handleUnsubscription(msg)
	case "ping":
		c.sendMessage(Message{Type: "pong", ID: msg.ID})
	default:
		c.sendError("Unknown message type")
	}
}

// handleSubscription handles subscription requests
func (c *Client) handleSubscription(msg SubscriptionMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, symbol := range msg.Symbols {
		// Add client to symbol subscription
		c.subscriptions[symbol] = true

		c.hub.mu.Lock()
		if c.hub.subscriptions[symbol] == nil {
			c.hub.subscriptions[symbol] = make(map[*Client]bool)
		}
		c.hub.subscriptions[symbol][c] = true
		c.hub.mu.Unlock()

		// Send initial order book snapshot
		if orderBook, err := c.hub.processor.GetOrderBook(symbol); err == nil {
			c.sendMessage(Message{
				Type:   "orderbook_snapshot",
				Symbol: symbol,
				Data:   orderBook,
				ID:     msg.ID,
			})
		}

		// Send initial market depth
		if depth, err := c.hub.processor.GetMarketDepth(symbol); err == nil {
			c.sendMessage(Message{
				Type:   "market_depth",
				Symbol: symbol,
				Data:   depth,
				ID:     msg.ID,
			})
		}
	}

	c.sendMessage(Message{
		Type: "subscription_confirmed",
		Data: map[string]interface{}{
			"symbols": msg.Symbols,
			"feeds":   msg.Feeds,
		},
		ID: msg.ID,
	})
}

// handleUnsubscription handles unsubscription requests
func (c *Client) handleUnsubscription(msg SubscriptionMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, symbol := range msg.Symbols {
		delete(c.subscriptions, symbol)

		c.hub.mu.Lock()
		if clients, exists := c.hub.subscriptions[symbol]; exists {
			delete(clients, c)
			if len(clients) == 0 {
				delete(c.hub.subscriptions, symbol)
			}
		}
		c.hub.mu.Unlock()
	}

	c.sendMessage(Message{
		Type: "unsubscription_confirmed",
		Data: map[string]interface{}{
			"symbols": msg.Symbols,
		},
		ID: msg.ID,
	})
}

// sendMessage sends a message to the client
func (c *Client) sendMessage(msg Message) {
	data, err := json.Marshal(msg)
	if err != nil {
		c.hub.log.Errorf("Failed to marshal message: %v", err)
		return
	}

	select {
	case c.send <- data:
	default:
		close(c.send)
		delete(c.hub.clients, c)
	}
}

// sendError sends an error message to the client
func (c *Client) sendError(errorMsg string) {
	c.sendMessage(Message{
		Type:  "error",
		Error: errorMsg,
	})
}

// removeClientSubscriptions removes all subscriptions for a client
func (h *Hub) removeClientSubscriptions(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	client.mu.RLock()
	for symbol := range client.subscriptions {
		if clients, exists := h.subscriptions[symbol]; exists {
			delete(clients, client)
			if len(clients) == 0 {
				delete(h.subscriptions, symbol)
			}
		}
	}
	client.mu.RUnlock()
}

// broadcastToClients broadcasts a message to all connected clients
func (h *Hub) broadcastToClients(message []byte) {
	for client := range h.clients {
		select {
		case client.send <- message:
		default:
			close(client.send)
			delete(h.clients, client)
		}
	}
}

// broadcastMarketUpdates broadcasts market updates to subscribed clients
func (h *Hub) broadcastMarketUpdates() {
	h.mu.RLock()
	subscriptions := make(map[string]map[*Client]bool)
	for symbol, clients := range h.subscriptions {
		subscriptions[symbol] = make(map[*Client]bool)
		for client := range clients {
			subscriptions[symbol][client] = true
		}
	}
	h.mu.RUnlock()

	for symbol, clients := range subscriptions {
		// Get current order book
		if orderBook, err := h.processor.GetOrderBook(symbol); err == nil {
			message := Message{
				Type:   "orderbook_update",
				Symbol: symbol,
				Data:   orderBook,
			}

			data, err := json.Marshal(message)
			if err != nil {
				h.log.Errorf("Failed to marshal order book update: %v", err)
				continue
			}

			for client := range clients {
				select {
				case client.send <- data:
				default:
					close(client.send)
					delete(h.clients, client)
				}
			}
		}

		// Get current market depth
		if depth, err := h.processor.GetMarketDepth(symbol); err == nil {
			message := Message{
				Type:   "market_depth_update",
				Symbol: symbol,
				Data:   depth,
			}

			data, err := json.Marshal(message)
			if err != nil {
				h.log.Errorf("Failed to marshal market depth update: %v", err)
				continue
			}

			for client := range clients {
				select {
				case client.send <- data:
				default:
					close(client.send)
					delete(h.clients, client)
				}
			}
		}
	}
}

// BroadcastOrderBookEvent broadcasts an order book event to subscribed clients
func (h *Hub) BroadcastOrderBookEvent(symbol string, event orderbook.OrderBookEvent) {
	h.mu.RLock()
	clients, exists := h.subscriptions[symbol]
	if !exists {
		h.mu.RUnlock()
		return
	}

	clientList := make([]*Client, 0, len(clients))
	for client := range clients {
		clientList = append(clientList, client)
	}
	h.mu.RUnlock()

	message := Message{
		Type:   "orderbook_event",
		Symbol: symbol,
		Data: map[string]interface{}{
			"event_type": eventTypeToString(event.Type),
			"timestamp":  event.Timestamp,
			"order":      event.Order,
			"execution":  event.Execution,
		},
	}

	data, err := json.Marshal(message)
	if err != nil {
		h.log.Errorf("Failed to marshal order book event: %v", err)
		return
	}

	for _, client := range clientList {
		select {
		case client.send <- data:
		default:
			close(client.send)
			delete(h.clients, client)
		}
	}
}

func generateClientID() string {
	return fmt.Sprintf("client_%d", time.Now().UnixNano())
}

func eventTypeToString(eventType orderbook.OrderBookEventType) string {
	switch eventType {
	case orderbook.OrderAdded:
		return "ORDER_ADDED"
	case orderbook.OrderCanceled:
		return "ORDER_CANCELED"
	case orderbook.OrderMatched:
		return "ORDER_MATCHED"
	case orderbook.OrderModified:
		return "ORDER_MODIFIED"
	default:
		return "UNKNOWN"
	}
}
