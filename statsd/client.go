// statsd/client.go
// Zoos Global — Microsoft DNS Monitor for Datadog
// https://www.zoosglobal.com
package statsd

import (
	"fmt"
	"net"
	"strings"
)

const maxPacketBytes = 1300 // stay safely below 1500-byte MTU

// Client is a fire-and-forget UDP DogStatsD client.
type Client struct {
	conn       *net.UDPConn
	addr       *net.UDPAddr
	globalTags []string
}

// New creates a connected UDP DogStatsD client.
func New(host string, port int, globalTags []string) (*Client, error) {
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", host, port))
	if err != nil {
		return nil, fmt.Errorf("resolving DogStatsD address: %w", err)
	}
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return nil, fmt.Errorf("connecting to DogStatsD %s:%d: %w", host, port, err)
	}
	return &Client{conn: conn, addr: addr, globalTags: globalTags}, nil
}

// Close releases the UDP socket.
func (c *Client) Close() {
	if c.conn != nil {
		c.conn.Close()
	}
}

// Gauge sends a gauge metric.
func (c *Client) Gauge(name string, value float64, tags []string) {
	c.send(c.format(name, value, "g", tags))
}

// Count sends a counter metric.
func (c *Client) Count(name string, value float64, tags []string) {
	c.send(c.format(name, value, "c", tags))
}

// Distribution sends a distribution metric (P50/P95/P99 in Datadog).
func (c *Client) Distribution(name string, value float64, tags []string) {
	c.send(c.format(name, value, "d", tags))
}

// Flush sends a batch of pre-formatted metric lines efficiently,
// splitting into multiple UDP packets when the payload exceeds maxPacketBytes.
func (c *Client) Flush(lines []string) {
	if len(lines) == 0 {
		return
	}
	buf := ""
	for _, line := range lines {
		if line == "" {
			continue
		}
		candidate := line
		if buf != "" {
			candidate = buf + "\n" + line
		}
		if len(candidate) > maxPacketBytes && buf != "" {
			c.send(buf)
			buf = line
		} else {
			buf = candidate
		}
	}
	if buf != "" {
		c.send(buf)
	}
}

// format builds a DogStatsD wire-format string.
// metric.name:value|type|#tag1,tag2
func (c *Client) format(name string, value float64, mtype string, tags []string) string {
	allTags := append(c.globalTags, tags...)
	tagStr := ""
	if len(allTags) > 0 {
		tagStr = "|#" + strings.Join(allTags, ",")
	}
	return fmt.Sprintf("%s:%g|%s%s", name, value, mtype, tagStr)
}

// send writes a single UDP datagram.
func (c *Client) send(payload string) {
	if c.conn == nil || payload == "" {
		return
	}
	_ = c.conn.SetWriteBuffer(64 * 1024)
	_, _ = c.conn.Write([]byte(payload))
}

// Line builds a pre-formatted metric line (used for batch collection).
func (c *Client) Line(name string, value float64, mtype string, tags []string) string {
	return c.format(name, value, mtype, tags)
}
