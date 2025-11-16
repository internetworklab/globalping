package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
)

func handleClient(conn net.Conn, clientID int) {
	defer conn.Close()

	log.Printf("Client %d connected from %s\n", clientID, conn.RemoteAddr())

	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)

	for {
		// 读取客户端消息
		message, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				log.Printf("Client %d disconnected\n", clientID)
			} else {
				log.Printf("Error reading from client %d: %v\n", clientID, err)
			}
			return
		}

		log.Printf("Received from client %d: %s", clientID, message)

		// Echo 回客户端
		response := fmt.Sprintf("Echo (from client %d): %s", clientID, message)
		_, err = writer.WriteString(response)
		if err != nil {
			log.Printf("Error writing to client %d: %v\n", clientID, err)
			return
		}
		writer.Flush()
	}
}

// returns a channel that can only be read from (not written to)
func connectionSpawner(listener net.Listener) <-chan net.Conn {
	connCh := make(chan net.Conn)

	go func() {
		defer close(connCh)

		for {
			conn, err := listener.Accept()
			if err != nil {
				if !errors.Is(err, net.ErrClosed) {
					log.Printf("Error accepting connection: %v", err)
				}
				log.Println("Exitting connection spawner...")
				return
			}
			connCh <- conn
		}
	}()

	return connCh
}

func main() {
	// 清理可能存在的旧 socket 文件
	socketPath := "/var/run/myserver.sock"
	os.Remove(socketPath)

	// 创建 Unix Socket 监听器
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		log.Fatal("Error listening:", err)
	}
	defer listener.Close()

	log.Printf("Server listening on %s\n", socketPath)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	clientCounter := 0
	connCh := connectionSpawner(listener)

	for {
		select {
		case sig := <-sigCh:
			log.Printf("Received signal: %s", sig.String())
			log.Println("\nShutting down server...")
			return
		case cliConn := <-connCh:
			log.Printf("Accepted connection, client id: %d", clientCounter)
			clientCounter++
			go handleClient(cliConn, clientCounter)
		}
	}
}
