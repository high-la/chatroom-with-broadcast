package chatroom

import (
	"fmt"
	"time"
)

func NewChatRoom(dataDir string) (*ChatRoom, error) {

	cr := &ChatRoom{
		clients:       make(map[*Client]bool),
		join:          make(chan *Client),
		leave:         make(chan *Client),
		broadcast:     make(chan string),
		listUsers:     make(chan *Client),
		directMessage: make(chan DirectMessage),
		sessions:      make(map[string]*SessionInfo),
		messages:      make([]Message, 0),
		startTime:     time.Now(),
		dataDir:       dataDir,
	}

	// Restore from snapshot if available
	if err := cr.loadSnapshot(); err != nil {
		fmt.Printf("Failed to load snapshot: %v\n", err)
	}

	// Initialize WAL for new messages
	if err := cr.initializePersistence(); err != nil {
		return nil, err
	}

	// Start background snapshot worker
	go cr.periodicSnapshots()

	return cr, nil
}

func (cr *ChatRoom) periodicSnapshots() {

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		cr.messageMu.Lock()
		messageCount := len(cr.messages)
		cr.messageMu.Unlock()

		if messageCount > 100 {
			if err := cr.createSnapshot(); err != nil {
				fmt.Printf("Snapshot failed: %v\n", err)
			}
		}
	}
}
