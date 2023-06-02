package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sync"
)

type Config struct {
	IP   string `json:"ip"`
	Port string `json:"port"`
	Fifo string `json:"fifo"`
}

var GlobalStats = Stats{
	ClientCount: 0,
}

type Stats struct {
	ClientCount int
}

type StreamWriter struct {
	sync.RWMutex
	clients map[chan []byte]bool
}

type ChanReader struct {
	ch chan []byte
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: boombox2 <configFile>")
		return
	}

	configFile := os.Args[1]

	file, err := os.Open(configFile)
	if err != nil {
		fmt.Println("error opening config file:", err)
		return
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	config := Config{}
	err = decoder.Decode(&config)
	if err != nil {
		fmt.Println("error reading config file:", err)
		return
	}

	addr := net.JoinHostPort(config.IP, config.Port)

	stream := NewStreamWriter()
	go streamAudio(stream, config.Fifo)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		clientChan := stream.AddClient()
		defer stream.RemoveClient(clientChan)

		w.Header().Set("Content-Type", "audio/mpeg")
		io.Copy(w, &ChanReader{ch: clientChan})
	})

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Println("error starting server:", err)
		return
	}
	fmt.Printf("Server started at %s\n", addr)

	http.Serve(ln, nil)
}

func (sw *StreamWriter) Write(p []byte) (n int, err error) {
	sw.RLock()
	defer sw.RUnlock()

	for client := range sw.clients {
		select {
		case client <- p:
		default:
		}
	}

	return len(p), nil
}

func (sw *StreamWriter) AddClient() chan []byte {
	GlobalStats.ClientCount++
	sw.Lock()

	defer sw.Unlock()

	client := make(chan []byte, 100)
	sw.clients[client] = true
	return client
}

func (sw *StreamWriter) RemoveClient(client chan []byte) {
	GlobalStats.ClientCount--
	sw.Lock()
	defer sw.Unlock()

	delete(sw.clients, client)
	close(client)
}

func NewStreamWriter() *StreamWriter {
	return &StreamWriter{
		clients: make(map[chan []byte]bool),
	}
}

func (r *ChanReader) Read(p []byte) (n int, err error) {
	data, ok := <-r.ch
	if !ok {
		return 0, io.EOF
	}
	return copy(p, data), nil
}

func streamAudio(sw *StreamWriter, fifo string) {
	bufSize := 8192
	buffer := make([]byte, bufSize)

	audioPipe, err := os.Open(fifo)
	if err != nil {
		fmt.Println("error opening audio pipe:", err)
		return
	}
	defer audioPipe.Close()

	for {
		n, err := audioPipe.Read(buffer)
		if err != nil && err != io.EOF {
			fmt.Println("error reading audio pipe:", err)
			break
		}

		if n == 0 {
			break
		}

		sw.Write(buffer[:n])
	}
}
