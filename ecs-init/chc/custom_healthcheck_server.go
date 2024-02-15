package chc

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"strconv"
	"time"

	log "github.com/cihub/seelog"
)

const (
	ecsInitSocket = "/var/run/ecs.sock"
	hcCmdTimeout  = time.Second * 30
)

func StartCustomHealthCheckServer() {
	err := os.Remove(ecsInitSocket) // remove any previous socket file
	if err != nil {
		log.Errorf("Error cleaning up old unix socket file: %v, err: %v", ecsInitSocket, err)
		return
	}

	// start listening
	l, err := net.ListenUnix("unix", &net.UnixAddr{ecsInitSocket, "unix"})
	if err != nil {
		log.Errorf("Error listening on the unix socket: %v, err: %v", ecsInitSocket, err)
		return
	}
	defer l.Close()

	log.Infof("Listening on %v ...", ecsInitSocket)

	for {
		conn, err := l.Accept()
		if err != nil {
			log.Errorf("Error accepting new client: %v", err)
			continue
		}
		log.Infof("New client connected.")
		handleConnectionErr := handleConnection(conn)
		if handleConnectionErr != nil {
			log.Errorf("Error handling connection: %v", err)
			continue
		}
	}
}

func handleConnection(conn net.Conn) error {
	defer conn.Close()

	// read the request
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		log.Errorf("Error reading: %v", err)
		return err
	}
	request := string(buf[0:n])
	log.Infof("Received a custom healthcheck cmd to run: %v", request)

	// execute the custom healthcheck command
	ctx, cancel := context.WithTimeout(context.Background(), hcCmdTimeout)
	cmd := exec.CommandContext(ctx, request)
	defer cancel()
	err = cmd.Start()
	if err != nil {
		log.Errorf("Error starting the custom healthcheck command: %v, err: %v", cmd, err)
		return err
	}

	// parse the custom health check output/exit code and send a response back to the ECS agent
	response := "99"
	err = cmd.Wait()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			response = strconv.Itoa(exitErr.ExitCode())
		} else {
			log.Errorf("invalid error during cmd.Wait(): %v", err)
		}
	} else {
		response = "0"
	}

	log.Infof("Custom healthcheck command exit code: %v", response)
	log.Infof("Sending response: %v", response)

	_, err = conn.Write([]byte(response))
	if err != nil {
		log.Errorf("Error sending response: %v", err)
		return err
	}

	log.Infof("Finished processing the custom healthcheck command: %v", request)
	return nil
}
