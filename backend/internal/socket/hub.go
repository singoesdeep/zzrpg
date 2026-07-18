package socket

import (
	"sync"
)

type Hub struct {
	mu               sync.RWMutex
	clients          map[*Client]bool
	characterClients map[int64]*Client // Maps CharacterID -> Client connection

	Register   chan *Client
	Unregister chan *Client
	Broadcast  chan []byte
}

func NewHub() *Hub {
	return &Hub{
		clients:          make(map[*Client]bool),
		characterClients: make(map[int64]*Client),
		Register:         make(chan *Client),
		Unregister:       make(chan *Client),
		Broadcast:        make(chan []byte),
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.Register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()

		case client := <-h.Unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.Send)
				if client.CharacterID > 0 {
					delete(h.characterClients, client.CharacterID)
				}
			}
			h.mu.Unlock()

		case message := <-h.Broadcast:
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client.Send <- message:
				default:
					// If the buffer is full, unregister the client
					h.mu.RUnlock()
					h.Unregister <- client
					h.mu.RLock()
				}
			}
			h.mu.RUnlock()
		}
	}
}

func (h *Hub) AssociateCharacter(client *Client, charID int64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// If there's an existing active session for this character, close it
	if oldClient, exists := h.characterClients[charID]; exists && oldClient != client {
		// Disconnect older session (standard Metin2 override connection behavior)
		h.characterClients[charID] = client
		client.CharacterID = charID
		oldClient.CharacterID = 0
		h.Unregister <- oldClient
		return
	}

	h.characterClients[charID] = client
	client.CharacterID = charID
}

func (h *Hub) GetClientByCharacterID(charID int64) (*Client, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	client, exists := h.characterClients[charID]
	return client, exists
}
