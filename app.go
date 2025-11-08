package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/hpcloud/tail"
	"github.com/quic-go/quic-go"
)

var (
	filePath   = flag.String("file", "log.txt", "File to tail")
	serverAddr = flag.String("server", "neo.nullferatu.com:5140", "QUIC server address")
)

type SyslogLine struct {
	Timestamp string `json:"timestamp"`
	Hostname  string `json:"hostname"`
	Program   string `json:"program"`
	Pid       int    `json:"pid"`
	Message   string `json:"message"`
}

type App struct {
	Conn      quic.Connection
	InputFile string
	Hostname  string
	Pid       int
}

func (a *App) TailAndProcess() {
	// Config: Poll:true is useful for mounted filesystems, but can be high CPU.
	// Adjusted to standard tailing from end of file.
	t, err := tail.TailFile(a.InputFile, tail.Config{
		Follow:   true,
		ReOpen:   true,
		Poll:     true,
		Location: &tail.SeekInfo{Offset: 0, Whence: 2},
		Logger:   tail.DiscardingLogger,
	})
	if err != nil {
		log.Fatalf("Error starting tail on %s: %v", a.InputFile, err)
	}

	// Open one stream for sending logs
	stream, err := a.Conn.OpenStreamSync(context.Background())
	if err != nil {
		log.Printf("Error opening QUIC stream: %v", err)
		return
	}
	defer stream.Close()

	log.Println("Stream opened, sending logs...")

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case line, ok := <-t.Lines:
			if !ok {
				log.Println("Tail channel closed, exiting.")
				return
			}
			if line.Err != nil {
				log.Printf("Tail error: %v", line.Err)
				continue
			}

			// Prepare the log line
			sl := SyslogLine{
				Timestamp: time.Now().Format(time.RFC3339),
				Hostname:  a.Hostname,
				Program:   "teller",
				Pid:       a.Pid,
				Message:   line.Text,
			}

			data, err := json.Marshal(sl)
			if err != nil {
				log.Printf("Error marshalling JSON: %v", err)
				continue
			}
			data = append(data, '\n') // Append newline for server parsing

			// Write to QUIC stream
			// Note: Your server implementation expects the whole JSON in one Read().
			// If logs are huge, this might fragment and break the server parser.
			_, err = stream.Write(data)
			if err != nil {
				log.Printf("Error writing to stream (server might be down): %v", err)
				// In a robust app, you might try to reconnect here.
				return
			}

		case <-ticker.C:
			// Send the specific string your server looks for to ignore beats
			_, err := stream.Write([]byte("|beat|"))
			if err != nil {
				log.Printf("Heartbeat failed: %v", err)
				return
			}
		}
	}
}

func (a *App) InitQUICConnection(addr string) error {
	tlsConf := &tls.Config{
		InsecureSkipVerify: true, // Kept for your testing environment
		NextProtos:         []string{"rider-protocol"},
	}

	// Using KeepAlive so the connection doesn't die silently
	quicConf := &quic.Config{
		KeepAlivePeriod: 10 * time.Second,
		MaxIdleTimeout:  1 * time.Minute,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := quic.DialAddr(ctx, addr, tlsConf, quicConf)
	if err != nil {
		return fmt.Errorf("error dialing QUIC: %v", err)
	}
	a.Conn = conn
	return nil
}

func main() {
	flag.Parse()

	hostname, _ := os.Hostname()
	app := &App{
		InputFile: *filePath,
		Hostname:  hostname,
		Pid:       os.Getpid(),
	}

	log.Printf("Connecting to QUIC server at %s...", *serverAddr)
	if err := app.InitQUICConnection(*serverAddr); err != nil {
		log.Fatalf("Failed to initialize QUIC connection: %v", err)
	}
	defer app.Conn.CloseWithError(0, "client exiting")

	log.Printf("Tailing file: %s", app.InputFile)
	app.TailAndProcess()
}
