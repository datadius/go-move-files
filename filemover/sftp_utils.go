package filemover

import (
	"github.com/charmbracelet/log"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

func main() {
	var conn *ssh.Client

	// open an sftp session over an existing  ssh connection.
	client, err := sftp.NewClient(conn)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	// walk a directory
	w := client.Walk("/testfiles/")
	for w.Step() {
		if w.Err() != nil {
			continue
		}
		log.Print(w.Path())
	}

	// leave your mark
	f, err := client.Create("hello.txt")
	if err != nil {
		log.Fatal(err)
	}
	if _, err := f.Write([]byte("I just got here, where am I?")); err != nil {
		log.Fatal(err)
	}
	f.Close()

	// check it's there
	fi, err := client.Lstat("hello.txt")
	if err != nil {
		log.Fatal(err)
	}
	log.Print(fi)
}
