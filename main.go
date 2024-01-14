package main

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

var (
	connMutex   sync.RWMutex
	connections map[string]net.Conn
	clientMsgCh map[string]chan string // 用于接收来自每个TCP客户端的消息
)

const timeoutDuration = 2 * time.Second // 设置2秒的超时

func init() {
	connections = make(map[string]net.Conn)
	clientMsgCh = make(map[string]chan string)
}

func handleTCPConn(conn net.Conn) {
	defer conn.Close()

	clientID := uuid.New().String() // 为每个客户端连接生成UUID
	clientCh := make(chan string, 1)
	connMutex.Lock()
	connections[clientID] = conn
	clientMsgCh[clientID] = clientCh
	connMutex.Unlock()

	fmt.Printf("Client [%s] connected\n", clientID)
	reader := bufio.NewReader(conn)
	for {
		message, err := reader.ReadString('\n')
		if err != nil {
			fmt.Printf("Client [%s] read error: %s\n", clientID, err)
			break
		}
		select {
		case clientMsgCh[clientID] <- message: // 将消息发送到该客户端专用的通道
		default:
			// 通道满或无法立即发送消息时不会阻塞
		}
	}

	connMutex.Lock()
	delete(connections, clientID)
	delete(clientMsgCh, clientID)
	connMutex.Unlock()
	fmt.Printf("Client [%s] disconnected\n", clientID)
}

func startTCPServer() {
	listener, err := net.Listen("tcp", ":8081")
	if err != nil {
		panic(err)
	}
	fmt.Println("TCP Server listening on :8081")

	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Printf("Accept error: %s\n", err)
			continue
		}

		go handleTCPConn(conn)
	}
}

func sendMessageToClient(c *gin.Context) {
	clientID := c.Param("id")
	message := c.PostForm("message")

	connMutex.RLock()
	conn, ok := connections[clientID]
	clientCh, chOK := clientMsgCh[clientID]
	connMutex.RUnlock()

	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "No client with ID " + clientID})
		return
	}

	_, err := fmt.Fprintf(conn, message)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to send message to client"})
		return
	}

	if !chOK {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to find message channel for client"})
		return
	}

	// 使用select等待客户端消息或超时
	select {
	case response := <-clientCh:
		c.JSON(http.StatusOK, gin.H{"status": "Message received from client", "response": response})
	case <-time.After(timeoutDuration):
		c.JSON(http.StatusRequestTimeout, gin.H{"error": "No response from client, timeout reached"})
	}
}

func getClients(c *gin.Context) {
	connMutex.RLock()
	defer connMutex.RUnlock()

	clients := make(map[string]string)
	for id, conn := range connections {
		clients[id] = conn.RemoteAddr().String()
	}

	c.JSON(http.StatusOK, gin.H{"clients": clients})
}

func main() {
	go startTCPServer()

	r := gin.Default()
	r.GET("/clients", getClients)
	r.POST("/send/:id", sendMessageToClient)

	r.Run(":8080")
}
