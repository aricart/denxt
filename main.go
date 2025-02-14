package main

import (
	"bufio"
	"fmt"
	"os/exec"
	"runtime"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nuid"
)

const natsServer = "demo.nats.io:4222"

func main() {
	denoPath, err := exec.LookPath("deno")
	if err != nil {
		panic(err)
	}

	var process *exec.Cmd

	nc, err := nats.Connect(fmt.Sprintf("nats://%s", natsServer), nats.Name("dispatcher"))
	if err != nil {
		panic(err)
	}
	fmt.Println("Connected", nc.ConnectedUrl())

	// FIXME: this has to be re-initialized with each process
	subj := nuid.Next()
	fmt.Println("sub requests to", subj)

	var last time.Time
	ticker := time.NewTicker(time.Second * 10)
	go func() {
		for {
			select {
			case <-ticker.C:
				if time.Now().Sub(last) > time.Second*10 {
					if process == nil {
						return
					}
					fmt.Println("stopping denolet process")
					_ = nc.Publish(fmt.Sprintf("stop.%s", subj), nil)
				}
			}
		}
	}()

	sub, err := nc.Subscribe("denolet", func(m *nats.Msg) {
		last = time.Now()
		fmt.Println("received a work message")
		if process == nil {
			fmt.Println("starting denolet process")
			process = exec.Command(denoPath, "run", "-A", "service.ts", natsServer, subj)

			stdout, err := process.StdoutPipe()
			if err != nil {
				panic(err)
			}
			stderr, err := process.StderrPipe()
			if err != nil {
				panic(err)
			}

			if err := process.Start(); err != nil {
				panic(err)
			}

			go func() {
				err := process.Wait()
				if err != nil {
					fmt.Printf("process exited with error: %v\n", err)
				}
				process = nil
			}()

			// Goroutine to output stdout
			go func() {
				scanner := bufio.NewScanner(stdout)
				for scanner.Scan() {
					fmt.Printf("[DENO STDOUT] %s\n", scanner.Text())
				}
			}()

			// Goroutine to output stderr
			go func() {
				scanner := bufio.NewScanner(stderr)
				for scanner.Scan() {
					fmt.Printf("[DENO STDERR] %s\n", scanner.Text())
				}
			}()

			time.Sleep(time.Millisecond * 500)
			if _, err := nc.Request(fmt.Sprintf("ping.%s", subj), nil, time.Second*5); err != nil {
				// some retry logic we died
				fmt.Printf("ping request failed: %v\n", err)
			}
		}
		_ = nc.PublishRequest(fmt.Sprintf("work.%s", subj), m.Reply, m.Data)
	})
	if err != nil {
		panic(err)
	}

	fmt.Printf("Listening on %s for denolet requests\n", sub.Subject)

	runtime.Goexit()
}
