package main

import (
	"context"
	scp "github.com/bramvdbogaerde/go-scp"
	"github.com/bramvdbogaerde/go-scp/auth"
	"github.com/charmbracelet/log"
	"golang.org/x/crypto/ssh"
	//"io"
	"os"
)

func main() {
	clientConfig, err := auth.PasswordKey("claud", "tiger", ssh.InsecureIgnoreHostKey())
	if err != nil {
		log.Print("Failed to create config: ", err)
		return
	}

	client := scp.NewClient("localhost:2022", &clientConfig)

	err = client.Connect()
	if err != nil {
		log.Print("Failed to connect: ", err)
		return
	}

	f, err := os.Create("./testdata/hello.txt")
	if err != nil {
		log.Print("Failed to open: ", err)
		return
	}

	defer client.Close()

	defer f.Close()

	// if the connection requires a PTY, then it will not work
	err = client.CopyFromRemote(context.Background(), f, "hello.txt")
	if err != nil {
		log.Print("Failed to copy: ", err)
		return
	}
}
