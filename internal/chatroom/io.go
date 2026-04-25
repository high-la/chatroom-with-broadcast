package chatroom

import (
	"bufio"
	"fmt"
	"math/rand"
	"net"
	"strings"
	"time"
)

func handleClient(conn net.Conn, chatRoom *ChatRoom) {

	defer func() {

		if r := recover(); r != nil {
			fmt.Printf("Panic in handleClient: %v\n", r)
		}
		conn.Close()
	}()

	// Set initial timeout for username entry
	conn.SetReadDeadline(time.Now().Add(30 * time.Second))

	reader := bufio.NewReader(conn)

	// Prompt for username or reconnection
	conn.Write([]byte("Enter username (or 'reconnect:<username>:<token>'): \n"))

	input, err := reader.ReadString('\n')
	if err != nil {
		fmt.Println("Failed to read username:", err)
		return
	}

	input = strings.TrimSpace(input)

	var username string
	var reconnectToken string
	var isReconnecting bool

	// Parse reconnection attempt
	if strings.HasPrefix(input, "reconnect:") {
		parts := strings.Split(input, ":")
		if len(parts) == 3 {
			username = parts[1]
			reconnectToken = parts[2]
			isReconnecting = true
		} else {
			conn.Write([]byte("Invalid format. Use: reconnect:<username>:<token>\n"))
			return
		}
	} else {
		username = input
	}

	// Generate guest name if empty
	if username == "" {
		username = fmt.Sprintf("Guest%d", rand.Intn(1000))
	}

	// Validate reconnection or check for duplicate
	if isReconnecting {
		if chatRoom.validateReconnectToken(username, reconnectToken) {
			fmt.Printf("%s reconnected successfully \n", username)
			conn.Write([]byte(fmt.Sprintf("Welcome back, %s! \n", username)))
		} else {
			conn.Write([]byte("Invalid token or session expired.\n"))
			return
		}
	} else {
		// Prevent duplicate logins
		if chatRoom.isUsernameConnected(username) {
			conn.Write([]byte("Username already connected. Use reconnect if you lost connection.\n"))
			return
		}

		// Create or retrieve session
		chatRoom.sessionMu.Lock()
		existingSession := chatRoom.sessions[username]
		chatRoom.sessionMu.Unlock()

		if existingSession != nil {
			token := existingSession.ReconnectToken
			msg := fmt.Sprintf("Tip: Save this token: %s \n", token)
			msg += fmt.Sprintf("To reconnect: reconnect: %s:%s \n", username, token)
			conn.Write([]byte(msg))
		} else {
			session := chatRoom.createSession(username)
			token := session.ReconnectToken
			msg := fmt.Sprintf("Your token: %s \n", token)
			msg += fmt.Sprintf("To reconnect: reconnect : %s:%s \n", username, token)
			conn.Write([]byte(msg))
		}
	}

	// Create client object
	client := &Client{
		conn:           conn,
		username:       username,
		outgoing:       make(chan string, 10), // Buffered
		lastActive:     time.Now(),
		reconnectToken: reconnectToken,
		isSlowClient:   rand.Float64() < 0.1, // 10% chance for testing
	}

	// Clear timeout for normal operation
	conn.SetReadDeadline(time.Time{})

	// Notify chatroom
	chatRoom.join <- client

	// Send welcome message
	welcomeMsg := buildWelcomeMessage(username)
	conn.Write([]byte(welcomeMsg))

	// Start read/write loops
	go readMessages(client, chatRoom)
	writeMessages(client) // Blocks until disconnect

	// Update session on disconnect
	chatRoom.updateSessionActivity(username)
	chatRoom.leave <- client
}

func buildWelcomeMessage(username string) string {
	msg := fmt.Sprintf("Welcome, %s! \n", username)

	msg += "Commands:\n"
	msg += "	/users - List all users \n"
	msg += "	/history [N] - Show last N messages \n"
	msg += "	/msg <user> <msg> - Private message \n"
	msg += "	/token - Show your reconnect token \n"
	msg += "	/stats - Show your stats \n"
	msg += "	/quit - Leave \n"

	return msg
}

func readMessages(client *Client, chatChatRoom *ChatRoom) {

	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("Panic in readMessages for %s:%v \n", client.username, r)
		}
	}()

	reader := bufio.NewReader(client.conn)

	for {
		// Set 5-minute idle timeout
		// 5 minutes of idle time triggers auto-disconnect. This
		// prevents zombie connections from consuming resources
		client.conn.SetReadDeadline(time.Now().Add(5 * time.Minute))

		message, err := reader.ReadString('\n')
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				fmt.Printf("%s timed out \n", client.username)
			} else {
				fmt.Printf("%s disconnected: %v \n", client.username, err)
			}
			return
		}

		client.markActive() // Update activity timestamp

		message = strings.TrimSpace(message)
		if message == "" {
			continue
		}

		client.mu.Lock()
		client.messageRecv++
		client.mu.Unlock()

		// Process commands vs. regular messages
		if strings.HasPrefix(message, "/") {
			handleCommand(client, chatChatRoom, message)
			continue
		}

		// Regular message - format and broadcast
		formatted := fmt.Sprintf("[%s]: %s \n", client.username, message)
		chatChatRoom.broadcast <- formatted
	}
}

func writeMessages(client *Client) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("Panic in writeMessages for %s: %v \n", client.username, r)
		}
	}()

	writer := bufio.NewWriter(client.conn)

	for message := range client.outgoing {

		// Simulate slow client (testing mode)
		if client.isSlowClient {
			time.Sleep(time.Duration(rand.Intn(500)) * time.Millisecond)
		}

		_, err := writer.WriteString(message)
		if err != nil {
			fmt.Printf("Write error for %s: %v \n", client.username, err)
			return
		}

		err = writer.Flush()
		if err != nil {
			fmt.Printf("Flush error for %s: %v \n", client.username)
			return
		}
	}
}
