package main

import (
	"bufio"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nuid"
)

type Denolet struct {
	sync.Mutex
	id       string
	process  *exec.Cmd
	nc       *nats.Conn
	hostPort string
	sync.WaitGroup
	done bool
}

func NewDenoContainer(nc *nats.Conn, hostPort string) *Denolet {
	return &Denolet{nc: nc, hostPort: hostPort}
}

func (d *Denolet) ready(duration time.Duration) error {
	d.WaitGroup.Add(1)
	start := time.Now()
	for {
		if time.Since(start) > duration {
			return errors.New("timeout waiting for denolet to start")
		}
		subj := fmt.Sprintf("ping.%s", d.id)
		fmt.Println("pinging denolet", subj)
		if _, err := d.nc.Request(subj, nil, duration); err != nil {
			if errors.Is(err, nats.ErrNoResponders) {
				time.Sleep(time.Millisecond * 100)
				continue
			}
		} else {
			return nil
		}
	}
}

func (d *Denolet) Process(m *nats.Msg) error {
	return d.nc.PublishRequest(fmt.Sprintf("work.%s", d.id), m.Reply, m.Data)
}

func (d *Denolet) Wait() {
	d.WaitGroup.Wait()
}

func (d *Denolet) Stop() {
	d.Lock()
	defer d.Unlock()
	if d.done {
		return
	}
	d.done = true
	_ = d.nc.Publish(fmt.Sprintf("stop.%s", d.id), nil)
	_ = d.nc.Flush()
	if d.process != nil {
		_ = d.process.Wait()
	}
	d.process = nil
	d.WaitGroup.Done()
}

func (d *Denolet) Start(scriptPath string) error {
	d.Lock()
	defer d.Unlock()

	d.id = nuid.Next()
	fmt.Printf("starting denolet with id %s\n", d.id)

	d.process = exec.Command("deno", "run", "-A", scriptPath, d.hostPort, d.id)

	stdout, err := d.process.StdoutPipe()
	if err != nil {
		d.process = nil
		return err
	}
	stderr, err := d.process.StderrPipe()
	if err != nil {
		d.process = nil
		return err
	}

	if err := d.process.Start(); err != nil {
		d.process = nil
		return err
	}

	go func() {
		err := d.process.Wait()
		if err != nil {
			fmt.Printf("process exited with error: %v\n", err)
		}
		d.Stop()
	}()

	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			fmt.Printf("[DENO STDOUT] %s\n", scanner.Text())
		}
	}()

	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			fmt.Printf("[DENO STDERR] %s\n", scanner.Text())
		}
	}()

	return d.ready(time.Second * 10)
}

func main() {
	nc, err := nats.Connect(fmt.Sprintf("nats://%s", "demo.nats.io"), nats.Name("dispatcher"))
	if err != nil {
		panic(err)
	}
	fmt.Println("Connected", nc.ConnectedUrl())

	var denolet *Denolet

	var last time.Time
	ticker := time.NewTicker(time.Second * 10)
	go func() {
		for {
			select {
			case <-ticker.C:
				if time.Now().Sub(last) > time.Second*10 {
					if denolet == nil {
						return
					}
					fmt.Println("stopping denolet process")
					denolet.Stop()
				}
			}
		}
	}()

	sub, err := nc.Subscribe("denolet", func(m *nats.Msg) {
		last = time.Now()
		fmt.Println("received a work message")
		if denolet == nil {
			denolet = NewDenoContainer(nc, "demo.nats.io")
			if err := denolet.Start("service.ts"); err != nil {
				fmt.Printf("failed to start denolet: %v\n", err)
				denolet.Stop()
				return
			}
			go func() {
				denolet.Wait()
				denolet = nil
				fmt.Println("stopped denolet")
			}()
			fmt.Println("started denolet")
		}
		if err := denolet.Process(m); err != nil {
			fmt.Printf("failed to process message: %v\n", err)
			denolet.Stop()
		}
	})
	if err != nil {
		panic(err)
	}

	fmt.Printf("Listening on %s for denolet requests\n", sub.Subject)

	runtime.Goexit()
}
