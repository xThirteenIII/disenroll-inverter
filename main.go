package main

import (
	"context"
	tunnel "disenroll-inverter/src"
	"fmt"
	"log"
	"os"

	// Third party packages
	"github.com/joho/godotenv"
)

func main(){

	// Load environment variables from .env file
	err := godotenv.Load()
	if err != nil {
		log.Fatal("error loading .env file")
	}

	sshUser := os.Getenv("SSH_USER")
	sshPrivateKey := os.Getenv("SSH_PRIVATE_KEY_PATH")
	sshDestination := os.Getenv("SSH_DESTINATION")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fmt.Printf("Hey Manuel, this program will:\n")
	fmt.Printf("  * check if inverter is enrolled\n")
	fmt.Printf("  * delete record if it is\n")
	fmt.Printf("  * check if inverter exists\n")
	fmt.Printf("  * delete record if it does\n")
	fmt.Printf("  * double check both records have been deleted from db\n")
	fmt.Printf("  * check if inverter exists on AWS DynamoDB cache\n")
	fmt.Printf("  * delete record if it does\n\n\n")

	fmt.Printf("Getting $HOME directory...")
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("\nError while getting home directory. Here's why: %v", err)
	}
	fmt.Printf("Done\n")

	// Start SSH Tunnel
	// Setup tunnel, do not start it yet
	sshTunnel := tunnel.NewSSHTunnel(
		sshUser, // user@host, default port is 22 if not specified
		tunnel.PrivateKeyFile(homeDir+sshPrivateKey), // Auth via private key
		sshDestination,
	)

	// Start SSH Tunnel in its goroutine, use context for handling shutdown.
	fmt.Printf("Starting SSH Tunnel...")
	go func() {
		if err := sshTunnel.Start(ctx); err != nil {
			log.Fatalf("\nFailed to start SSH tunnel. Here's why: %v", err)
		}
	}()
	fmt.Printf("Done\n")
}
