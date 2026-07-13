package ws

import (
	"time"

	"github.com/gorilla/websocket"
)

type Client struct {
	conn *websocket.Conn
	send chan Event
	done chan struct{}
}

func NewClient(conn *websocket.Conn) *Client {
	return &Client{
		conn: conn,
		send: make(chan Event, 16),
		done: make(chan struct{}),
	}
}

func (c *Client) Send(event Event) {
	select {
	case c.send <- event:
	default:
		c.Close()
	}
}

func (c *Client) Close() {
	select {
	case <-c.done:
	default:
		close(c.done)
		_ = c.conn.Close()
	}
}

func (c *Client) WriteLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	defer c.Close()

	for {
		select {
		case event := <-c.send:
			if err := c.conn.WriteJSON(event); err != nil {
				return
			}
		case <-ticker.C:
			if err := c.conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)); err != nil {
				return
			}
		case <-c.done:
			return
		}
	}
}

func (c *Client) ReadLoop() {
	defer c.Close()

	for {
		if _, _, err := c.conn.NextReader(); err != nil {
			return
		}
	}
}
