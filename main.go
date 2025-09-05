package main

import (
	"context"
	"database/sql"
	tunnel "disenroll-inverter/src"
	"disenroll-inverter/src/dbops"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	// Third party packages
	"github.com/joho/godotenv"

	_ "github.com/go-sql-driver/mysql"
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

	// Wait for tunnel to signal readiness.
	if err := sshTunnel.WaitReady(ctx); err != nil {
		log.Fatalf("\nError waiting for tunnel. Here's why: %v", err)
	}
	fmt.Printf("Tunnel established successfully on %s.\n", sshTunnel.Local)

	// Open HeidiSQL Connection
	dbName := os.Getenv("DBNAME")
	dbUsername := os.Getenv("DB_USERNAME")
	dbPassword := os.Getenv("DB_PASSWORD")
	connStr := fmt.Sprintf("%s:%s@tcp(127.0.0.1:%d)/%s", dbUsername, dbPassword, sshTunnel.Local.Port, dbName)

	fmt.Printf("Connecting to HeidiSQL...")
	db, err := sql.Open("mysql", connStr)
	if err != nil {
		log.Fatalf("failed to open database. Here's why: %v", err)
	}
	defer db.Close()

	// Explicit ping timeout to bound waiting.
	ctxPing, cancelPing := context.WithTimeout(ctx, 5 * time.Second)
	defer cancelPing()
	if err := db.PingContext(ctxPing); err != nil {
		log.Fatalf("\nFailed to ping database: %v", err)
	}
	fmt.Printf("Done\n")

	// Start a watchdog that pings DB every 30 second, making sure connection keeps alive.
	go dbops.StartWatchdog(ctx, db, 30*time.Second)

	// Block main until signal of shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	select {
	case <- ctx.Done():
		log.Println("context canceled, shutting down...")
	case s := <- sigCh:
		log.Printf("\nReceived signal %s, shutting down...", s)
		cancel() // shut down context
	}
}
