package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/hpcloud/tail"
	"github.com/quic-go/quic-go"
)

var (
	syslogMode = flag.Bool("syslog", false, "Enable syslog logging")
	filePath   = flag.String("file", "log.txt", "File to tail")
)

type Logger interface {
	Process() ([]byte, error)
}

type SyslogLine struct {
	Beat      bool   `json:"beat"`
	Timestamp string `json:"timestamp"`
	Hostname  string `json:"hostname"`
	Program   string `json:"program"`
	Pid       string `json:"pid"`
	Message   string `json:"message"`
}

func (l *SyslogLine) Process() ([]byte, error) {
	return json.Marshal(l)
}

type App struct {
	Conn      quic.Connection
	ReqCh     chan SyslogLine
	Requsts   []SyslogLine
	Client    *http.Client
	InputFile string
	Memory    *sync.Mutex // Changed to sync.Mutex
	Lines     bytes.Buffer
}

func (a *App) TailAndProcess() {
	t, err := tail.TailFile(a.InputFile, tail.Config{
		Follow: true,
		ReOpen: true,
		Poll:   true,
	})
	if err != nil {
		fmt.Printf("Error tailing file: %v\n", err)
		return
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	stream, err := a.Conn.OpenStreamSync(context.Background())
	if err != nil {
		fmt.Printf("Error opening stream: %v\n", err)
		return
	}
	defer stream.Close()
	// go func() {
	for {
		select {
		case line, ok := <-t.Lines:
			if !ok {
				fmt.Println("Tail stopped")
				return
			}
			// fmt.Println("New line:", line.Text, len(line.Text))
			a.Memory.Lock()
			_, err := a.Lines.WriteString(line.Text + "\n")
			if err != nil {
				fmt.Printf("Error writing to buffer: %v\n", err)
				a.Memory.Unlock()
				continue
			}
			a.Memory.Unlock()

			sl := SyslogLine{
				Timestamp: time.Now().String(),
				Hostname:  "localhost",
				Program:   "myapp",
				Pid:       "1234",
				Message:   line.Text,
			}
			data, err := json.Marshal(sl)
			if err != nil {
				fmt.Printf("Error marshalling syslog line: %v\n", err)
				continue
			}
			_, err = stream.Write(data)
			if err != nil {
				a.AddRequest(sl)
				fmt.Printf("Error writing to stream: %v\n", err)
				return
			}
			chunk := make([]byte, 4096)
			n, err := stream.Read(chunk)
			if err != nil {
				fmt.Printf("Error reading from stream: %v\n", err)
				return
			}
			if n < 1 {
				// fmt.Printf("Received: %s\n", string(chunk[:n]))
				fmt.Print(" got one ")
			}
		case <-ticker.C:
			// fmt.Println("Sending heartbeat...")
			data, err := json.Marshal(SyslogLine{
				Beat:      true,
				Timestamp: time.Now().String(),
			})
			if err != nil {
				fmt.Printf("Error marshalling beat data: %v\n", err)
				continue
			}
			_, err = stream.Write(data)
			if err != nil {
				fmt.Printf("Error writing to stream: %v\n", err)
				return
			}
			chunk := make([]byte, 4096)
			n, err := stream.Read(chunk)
			if err != nil {
				fmt.Printf("Error reading from stream: %v\n", err)
				return
			}
			if n < 1 {
				// fmt.Printf("Received: %s\n", string(chunk[:n]))
				fmt.Print(" got one ")
			}
		}
	}
	// }()
	// for {
	// 	chunk := make([]byte, 4096)
	// 	n, err := stream.Read(chunk)
	// 	if err != nil {
	// 		fmt.Printf("Error reading from stream: %v\n", err)
	// 		return
	// 	}
	// 	if n > 0 {
	// 		// fmt.Printf("Received: %s\n", string(chunk[:n]))
	// 		fmt.Print(" got one ")
	// 	}
	// }
}

func (a *App) AddRequest(req SyslogLine) {
	// limit := 150
	a.Memory.Lock()
	defer a.Memory.Unlock()
	// if len(a.Requsts) >= limit {
	// 	a.Requsts = a.Requsts[1:]
	// }
	a.Requsts = append(a.Requsts, req)
}

func (a *App) ProcessRequests() {
	fmt.Println("Processing requests...")
	// wg := sync.WaitGroup{}
	stream, err := a.Conn.OpenStreamSync(context.Background())
	if err != nil {
		fmt.Printf("Error opening stream: %v\n", err)
		return
	}
	defer stream.Close()
	a.Memory.Lock()
	defer a.Memory.Unlock()
}

func main() {
	flag.Parse()

	app := &App{
		ReqCh:     make(chan SyslogLine, 500),
		Requsts:   make([]SyslogLine, 500),
		Client:    &http.Client{},
		InputFile: *filePath,
		Memory:    &sync.Mutex{}, // Initialize sync.Mutex
	}
	if err := app.InitQUICConnection(); err != nil {
		log.Fatalf("Failed to initialize QUIC connection: %v", err)
	}

	fmt.Printf("Tailing file: %s (syslog mode: %v)\n", app.InputFile, *syslogMode)
	app.TailAndProcess()
}

func (a *App) InitQUICConnection() error {
	tlsConf := &tls.Config{
		InsecureSkipVerify: true, // ONLY for testing
		NextProtos:         []string{"quic-log-protocol"},
	}

	conn, err := quic.DialAddr(context.Background(), "mrbyte:8081", tlsConf, nil)
	if err != nil {
		return fmt.Errorf("error dialing QUIC: %v", err)
	}
	a.Conn = conn
	return nil
}
