package socket

import (
	"context"
	"sync"

	"github.com/singoesdeep/zzrpg/backend/engine/bus"
	"github.com/singoesdeep/zzrpg/backend/internal/character"
)

type Hub struct {
	mu               sync.RWMutex
	clients          map[*Client]bool
	characterClients map[int64]*Client // Maps CharacterID -> Client connection

	Register   chan *Client
	Unregister chan *Client
	Broadcast  chan []byte

	eventBus bus.EventBus
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

// SetEventBus attaches an engine bus so the hub can publish session-lifecycle
// events (CharacterLoggedOut). Optional and nil-safe: without it the hub behaves
// exactly as before.
func (h *Hub) SetEventBus(b bus.EventBus) { h.eventBus = b }

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.Register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()

		case client := <-h.Unregister:
			h.removeClient(client)

		case message := <-h.Broadcast:
			// Collect clients whose send buffer is full without mutating the
			// hub from inside this loop. We must NOT send to h.Unregister here:
			// Run is the sole reader of that channel, so a send from within this
			// case would block forever (self-deadlock, freezing the whole hub).
			var stale []*Client
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client.Send <- message:
				default:
					stale = append(stale, client)
				}
			}
			h.mu.RUnlock()

			// Drop slow consumers after releasing the read lock.
			for _, client := range stale {
				h.removeClient(client)
			}
		}
	}
}

// removeClient deregisters a client and closes its send channel. It is
// idempotent: the clients-map membership check guards against a double close
// when a client is removed both by a broadcast drop and its ReadPump defer.
func (h *Hub) removeClient(client *Client) {
	h.mu.Lock()
	var loggedOut int64
	if _, ok := h.clients[client]; ok {
		delete(h.clients, client)
		close(client.Send)
		if client.CharacterID > 0 {
			loggedOut = client.CharacterID
			delete(h.characterClients, client.CharacterID)
		}
	}
	h.mu.Unlock()

	// Publish the logout outside the lock; the bus is async and nil-safe.
	if loggedOut > 0 && h.eventBus != nil {
		_ = h.eventBus.Publish(context.Background(), character.CharacterLoggedOut{CharacterID: loggedOut})
	}
}

func (h *Hub) AssociateCharacter(client *Client, charID int64) {
	h.mu.Lock()
	oldClient, exists := h.characterClients[charID]
	h.characterClients[charID] = client
	client.CharacterID = charID
	if exists && oldClient != client {
		// Zero the old client's CharacterID so the removeClient call below does
		// not delete the freshly-set characterClients[charID] mapping.
		oldClient.CharacterID = 0
	} else {
		oldClient = nil
	}
	h.mu.Unlock()

	// Disconnect the older session (connection-override behavior) outside the
	// lock. Sending to h.Unregister while holding h.mu would deadlock against
	// Run's Unregister handler, which itself needs h.mu.
	if oldClient != nil {
		h.removeClient(oldClient)
	}
}

func (h *Hub) GetClientByCharacterID(charID int64) (*Client, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	client, exists := h.characterClients[charID]
	return client, exists
}
