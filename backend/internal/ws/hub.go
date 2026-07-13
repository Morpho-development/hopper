package ws

import "sync"

type Event struct {
	Type    string `json:"type"`
	Payload any    `json:"payload"`
}

type Hub struct {
	mu      sync.RWMutex
	clients map[string]map[*Client]struct{}
}

func NewHub() *Hub {
	return &Hub{clients: make(map[string]map[*Client]struct{})}
}

func (h *Hub) Register(userID string, client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.clients[userID] == nil {
		h.clients[userID] = make(map[*Client]struct{})
	}
	h.clients[userID][client] = struct{}{}
}

func (h *Hub) Unregister(userID string, client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	userClients := h.clients[userID]
	if userClients == nil {
		return
	}
	delete(userClients, client)
	if len(userClients) == 0 {
		delete(h.clients, userID)
	}
}

func (h *Hub) Broadcast(userID string, event Event) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for client := range h.clients[userID] {
		client.Send(event)
	}
}
