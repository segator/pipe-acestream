package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

const aceStreamEnginePath = "/opt/acestream/acestreamengine"

func main() {
	log.SetOutput(os.Stderr)
	sigChan := make(chan os.Signal)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		log.Println(" got interrupt signal: ", <-sigChan)
		cancel()
	}()
	acestreamID := os.Args[1]
	httpPort := 6878
	cmd := exec.Command(aceStreamEnginePath, "--client-console", "--bind-all")
	cmd.Env = os.Environ()
	aceStdout, err := cmd.StdoutPipe()
	if err != nil {
		os.Exit(1)
	}

	stder, err := cmd.StderrPipe()
	if err != nil {
		os.Exit(1)
	}
	go func() {
		io.Copy(io.Discard, stder)
	}()
	err = cmd.Start()
	if err != nil {
		os.Exit(1)
	}
	go func() {
		<-ctx.Done()
		cmd.Process.Signal(syscall.SIGKILL)
	}()

	if !waitForServerReady(ctx, aceStdout) {
		sigChan <- syscall.SIGTERM
		//log.Fatal("acestream were not initialized correctly")
		os.Exit(1)
	}

	go func() {
		for {
			err = readStream(ctx, httpPort, acestreamID)
			if err != nil {
				log.Println(err)
			}
		}
	}()
	cmd.Wait()
}

func readStream(ctx context.Context, httpPort int, aceStreamID string) error {
	client := &http.Client{}
	url := fmt.Sprintf("http://localhost:%d/ace/getstream?id=%s", httpPort, aceStreamID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	req.Header.Add("Accept", "*/*")
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	io.Copy(os.Stdout, res.Body)
	return nil
}
func waitForServerReady(ctx context.Context, reader io.ReadCloser) bool {
	bufreader := bufio.NewReader(reader)

	msgChan := make(chan string)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				line, _, err := bufreader.ReadLine()
				if err != nil {
					break
				}
				msg := string(line)
				//log.Println(msg)
				msgChan <- msg
			}
		}
		close(msgChan)
	}()
	for {
		select {
		case <-ctx.Done():
			return false
		case msg, ok := <-msgChan:
			if !ok {
				return false
			}
			if strings.Contains(msg, "acestream.VideoServer|start: addr= port=6878 allow_remote=1 allow_intranet=1") {
				return true
			}
		case <-time.After(time.Second * 10):
			return false
		}
	}

	return false
}
